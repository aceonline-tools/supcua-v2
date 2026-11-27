import { ApiError, type Client } from '../api/client';
import { recordEvent, sequenceOf, type KeyPressAction } from './keySequence';
import type { Action, ActionSpec, CaptureEndReason, HotkeyConflict, HotkeyField, Script } from '../api/types';
import type { AppStore } from './store.svelte';
import { dropIndex, insertIndex } from './reorder';

export type CloseResult = 'closed' | 'conflict' | 'failed' | 'noop';
type SaveResult = 'ok' | 'stale' | 'conflict' | 'name' | 'failed';
const MAX_SAVE_PASSES = 5;
type CaptureEventName = 'hotkey' | 'key' | 'mouse' | 'ended';
export type Capturing = HotkeyField | 'key' | 'keys' | null;

export interface ActionDialogState {
  index: number;
  action: Action;
  spec: ActionSpec;
}

export const FIELD_LABEL: Record<HotkeyConflict['field'], string> = { toggle_hotkey: 'Toggle', start_hotkey: 'Start', stop_hotkey: 'Stop' };

export class EditorStore {
  sessionId = $state<string | null>(null);
  script = $state<Script | null>(null);
  catalog = $state<ActionSpec[]>([]);
  labels = $state<string[]>([]);
  selected = $state(-1);
  dirty = $state(false);
  saveFailed = $state(false);
  mouse = $state({ x: 0, y: 0 });
  builder = $state({ key: '', delay: 100, loop: 1 });
  capturing = $state<Capturing>(null);
  recordMods = $state<string[]>([]);
  recordNote = $state('');
  conflicts = $state<HotkeyConflict[] | null>(null);
  actionDialog = $state<ActionDialogState | null>(null);
  raw = $state<{ text: string; error: string } | null>(null);
  private closing = false;
  private closePromise: Promise<CloseResult> | null = null;
  private gen = 0;
  discardPrompt = $state<string | null>(null);
  // Host-side capture is single-mode (bridge/capture.go). Every start returns a monotonic capture_id and
  // every capture event carries one; only events for the current capture are acted on. Transitions are
  // serialized on `chain`, and events that arrive while a start is in flight are buffered until its
  // capture_id is known.
  private curId: number | null = null;
  private curKind: 'mouse' | 'hotkey' | 'key' | null = null;
  private lastId = 0;
  private starting = false;
  private buffered: { name: CaptureEventName; e: any }[] = [];
  private chain: Promise<unknown> = Promise.resolve();
  private needResume = false;
  // Capture intent (`capturing`) belongs to one request (`capturingOp`) and is set only when that request's
  // queued transition runs, so an earlier completion can never clear a later request's intent.
  private opSeq = 0;
  private curOp: number | null = null;
  private capturingOp: number | null = null;
  private lastReqOp = 0;
  private pendingStarts = 0;

  constructor(private api: Client, private app: AppStore) {
    api.on('capture.hotkey', (e) => this.onCaptureEvent('hotkey', e));
    api.on('capture.key', (e) => this.onCaptureEvent('key', e));
    api.on('capture.mouse', (e) => this.onCaptureEvent('mouse', e));
    api.on('capture.ended', (e) => this.onCaptureEvent('ended', e));
    api.on('editor.reset', (e) => this.onReset(e.session_id));
    if (typeof window !== 'undefined') window.addEventListener('focus', () => this.onActivated());
  }

  get indicator(): { saved: boolean; text: string } {
    const sc = this.script;
    if (!sc) return { saved: true, text: 'Saved' };
    const name = sc.name.trim();
    if (!name) return { saved: false, text: 'Not saved: name required' };
    if (this.app.scripts.some((x) => x.id !== sc.id && x.name.toLowerCase() === name.toLowerCase())) return { saved: false, text: 'Not saved: name taken' };
    if (this.saveFailed) return { saved: false, text: 'Not saved: save failed' };
    return this.dirty ? { saved: false, text: 'Not saved' } : { saved: true, text: 'Saved' };
  }

  async open(id: string | null): Promise<void> {
    if (this.sessionId) {
      if (id !== null && id === this.script?.id) this.app.route = { name: 'editor', id };
      else this.app.error = 'Another editor is already open. Close it first.';
      return;
    }
    try {
      const r = await this.api.call('editor.open', { id });
      this.sessionId = r.sessionId;
      this.script = r.script;
      this.catalog = r.catalog;
      // A new script has never been saved: closing must persist it (autosave-on-close parity).
      this.dirty = id === null;
    } catch (e) {
      this.app.error = e instanceof ApiError ? (e.code === 'EDITOR_BUSY' ? 'Another editor is already open.' : e.message) : 'Failed to open editor';
      return;
    }
    this.app.route = { name: 'editor', id };
    await this.refreshLabels();
    await this.enqueue(() => this.doResume());
  }

  markDirty(): void {
    this.gen++;
    this.dirty = true;
    this.saveFailed = false;
  }

  private async refreshLabels(): Promise<void> {
    if (!this.script) return;
    try {
      this.labels = await this.api.call('editor.describeActions', { actions: $state.snapshot(this.script.actions) });
    } catch {
      this.labels = this.script.actions.map((a) => a.type);
    }
  }

  private async changed(): Promise<void> {
    this.markDirty();
    await this.refreshLabels();
  }

  get insertAt(): number { return insertIndex(this.selected, this.script?.actions.length ?? 0); }

  async insert(action: Action): Promise<void> {
    if (!this.script) return;
    const at = this.insertAt;
    this.script.actions.splice(at, 0, action);
    this.selected = at;
    await this.changed();
  }

  addSimple(type: string): Promise<void> { return this.insert({ type }); }
  addMouseMove(): Promise<void> { return this.insert({ type: 'mouse_move', x: this.mouse.x, y: this.mouse.y }); }
  async addKey(type: 'key_press' | 'key_down' | 'key_up'): Promise<void> { if (this.builder.key) await this.insert({ type, key: this.builder.key }); }
  async addDelay(): Promise<void> { if (this.builder.delay > 0) await this.insert({ type: 'delay', ms: Math.trunc(this.builder.delay) }); }
  addLoop(): Promise<void> { return this.insert({ type: 'loop', count: Math.max(0, Math.trunc(this.builder.loop)) }); }

  selectAction(i: number): void { this.selected = i; }

  editSelected(): void {
    const a = this.script?.actions[this.selected];
    const spec = this.catalog.find((c) => c.type === a?.type);
    if (!a || !spec || spec.params.length === 0) return;
    this.actionDialog = { index: this.selected, action: structuredClone($state.snapshot(a)), spec };
  }

  async applyActionDialog(action: Action): Promise<void> {
    const d = this.actionDialog;
    this.actionDialog = null;
    if (!d || !this.script) return;
    this.script.actions[d.index] = action;
    await this.changed();
  }

  closeActionDialog(): void {
    this.actionDialog = null;
    if (this.capturing === 'key' || this.capturing === 'keys') this.cancelCapture();
  }

  async deleteSelected(): Promise<void> {
    const sc = this.script;
    if (!sc || this.selected < 0 || this.selected >= sc.actions.length) return;
    sc.actions.splice(this.selected, 1);
    this.selected = Math.min(this.selected, sc.actions.length - 1);
    await this.changed();
  }

  async move(delta: -1 | 1): Promise<void> {
    const sc = this.script;
    const from = this.selected;
    const to = from + delta;
    if (!sc || from < 0 || to < 0 || to >= sc.actions.length) return;
    [sc.actions[from], sc.actions[to]] = [sc.actions[to], sc.actions[from]];
    this.selected = to;
    await this.changed();
  }

  async moveTo(from: number, gap: number): Promise<void> {
    const sc = this.script;
    if (!sc || from < 0 || from >= sc.actions.length) return;
    const to = dropIndex(from, Math.max(0, Math.min(sc.actions.length, gap)));
    if (to === from) return;
    const [a] = sc.actions.splice(from, 1);
    sc.actions.splice(to, 0, a);
    this.selected = to;
    await this.changed();
  }

  private enqueue(fn: () => Promise<void>): Promise<void> {
    const run = this.chain.then(fn);
    this.chain = run.catch(() => {});
    return run.catch(() => {});
  }

  private async doStart(kind: 'mouse' | 'hotkey' | 'key', call: () => Promise<{ capture_id: number }>, op = ++this.opSeq): Promise<boolean> {
    const sid = this.sessionId;
    this.lastId = Math.max(this.lastId, this.curId ?? 0);
    this.curId = null;
    this.curKind = null;
    this.curOp = null;
    this.starting = true;
    this.buffered = [];
    try {
      const r = await call();
      if (this.sessionId !== sid) { this.starting = false; this.buffered = []; return false; }
      this.curId = r.capture_id;
      this.curKind = kind;
      this.curOp = op;
      this.lastId = Math.max(this.lastId, r.capture_id);
    } catch {
      this.starting = false;
      this.buffered = [];
      if (this.capturingOp === op) { this.capturing = null; this.capturingOp = null; }
      return false;
    }
    this.starting = false;
    const pending = this.buffered;
    this.buffered = [];
    for (const b of pending) if (b.e.capture_id === this.curId) this.apply(b.name, b.e);
    return true;
  }

  // Stops only the capture the caller scheduled the stop for (by id, or by the request that started it).
  private async doStop(sid: string | null, match: { id?: number; op?: number }): Promise<void> {
    const id = this.curId;
    if (!sid || sid !== this.sessionId || id === null) return;
    if (match.id !== undefined && match.id !== id) return;
    if (match.op !== undefined && match.op !== this.curOp) return;
    await this.api.call('capture.stop', { sessionId: sid, capture_id: id }).catch(() => {});
    if (this.curId === id) { this.curId = null; this.curKind = null; this.curOp = null; }
  }

  private async doResume(): Promise<void> {
    const sessionId = this.sessionId;
    if (!sessionId || this.closing || this.curId !== null || this.starting) return;
    this.needResume = false;
    await this.doStart('mouse', () => this.api.call('capture.mouse.start', { sessionId }));
  }

  private stopAndResume(match: { id?: number; op?: number }): Promise<void> {
    const sid = this.sessionId;
    return this.enqueue(async () => {
      await this.doStop(sid, match);
      await this.doResume();
    });
  }

  private requestStart(kind: 'hotkey' | 'key', intent: Capturing, call: (sessionId: string) => Promise<{ capture_id: number }>): Promise<void> {
    const sid = this.sessionId;
    if (!sid) return Promise.resolve();
    const op = ++this.opSeq;
    this.lastReqOp = op;
    this.pendingStarts++;
    return this.enqueue(async () => {
      this.pendingStarts--;
      if (this.sessionId !== sid) return;
      this.capturing = intent;
      this.capturingOp = op;
      if (!(await this.doStart(kind, () => call(sid), op))) await this.doResume();
    });
  }

  startCapture(field: HotkeyField): Promise<void> {
    return this.requestStart('hotkey', field, (sessionId) => this.api.call('capture.hotkey.start', { sessionId, field }));
  }

  startKeyCapture(): Promise<void> {
    return this.requestStart('key', 'key', (sessionId) => this.api.call('capture.key.start', { sessionId }));
  }

  // Records a key sequence into the open key_press dialog; the host keeps streaming key-downs
  // until Esc / Stop, and global hotkeys stay suspended for the whole capture.
  startKeyRecording(): Promise<void> {
    this.recordMods = [];
    this.recordNote = '';
    return this.requestStart('key', 'keys', (sessionId) => this.api.call('capture.key.start', { sessionId }));
  }

  capturePending(): boolean {
    return this.pendingStarts > 0;
  }

  // Cancels the most recent capture request (queued or active); later requests are unaffected.
  cancelCapture(): Promise<void> {
    if (!this.capturing && this.pendingStarts === 0) return Promise.resolve();
    const sid = this.sessionId;
    const target = this.pendingStarts > 0 ? this.lastReqOp : this.capturingOp ?? this.curOp;
    if (target === null) return Promise.resolve();
    return this.enqueue(async () => {
      if (this.capturingOp === target) { this.capturing = null; this.capturingOp = null; }
      await this.doStop(sid, { op: target });
      await this.doResume();
    });
  }

  onActivated(): void {
    if (this.needResume) void this.enqueue(() => this.doResume());
  }

  // After a close attempt leaves the editor open the host-side capture may have ended unnoticed.
  private reconcile(): void {
    if (!this.sessionId) return;
    if (this.capturing && this.curId === null && !this.starting && this.pendingStarts === 0) { this.capturing = null; this.capturingOp = null; }
    void this.enqueue(() => this.doResume());
  }

  private onCaptureEvent(name: CaptureEventName, e: { capture_id: number }): void {
    if (this.starting) {
      if (e.capture_id > this.lastId) this.buffered.push({ name, e });
      return;
    }
    if (e.capture_id !== this.curId) return;
    this.apply(name, e);
  }

  private apply(name: CaptureEventName, e: any): void {
    switch (name) {
      case 'hotkey': return this.onCaptureHotkey(e.field, e.hotkey);
      case 'key': return this.onCaptureKey(e.key, e.mods);
      case 'mouse': return this.onCaptureMouse(e.x, e.y);
      case 'ended': return this.onCaptureEnded(e.reason);
    }
  }

  private onCaptureEnded(reason: CaptureEndReason): void {
    if (this.capturingOp !== null && this.capturingOp === this.curOp) { this.capturing = null; this.capturingOp = null; }
    this.curId = null;
    this.curKind = null;
    this.curOp = null;
    if (!this.sessionId || reason === 'editorClosed') return;
    if (reason === 'blur') { this.needResume = true; return; }
    void this.enqueue(() => this.doResume());
  }

  private onReset(sessionId: string): void {
    if (!this.sessionId || sessionId !== this.sessionId) return;
    this.reset();
    this.app.route = { name: 'scripts' };
    this.app.error = 'The editor session was reset; unsaved changes were lost.';
    void this.app.loadScripts().catch(() => {});
  }

  clearHotkey(field: HotkeyField): void {
    if (!this.script) return;
    this.script[`${field}_hotkey` as const] = null;
    this.markDirty();
  }

  private onCaptureHotkey(field: HotkeyField, hotkey: { modifiers: string[]; key: string } | null): void {
    if (!this.script || this.curKind !== 'hotkey' || this.capturing !== field || this.capturingOp !== this.curOp) return;
    if (!hotkey || hotkey.key.toLowerCase() === 'esc' || hotkey.key.toLowerCase() === 'escape') { void this.cancelCapture(); return; }
    this.script[`${field}_hotkey` as const] = hotkey;
    this.capturing = null;
    this.capturingOp = null;
    this.markDirty();
    void this.stopAndResume({ id: this.curId ?? undefined });
  }

  private onCaptureKey(key: string, mods?: string[] | null): void {
    if (this.curKind === 'key' && this.capturing === 'keys' && this.capturingOp === this.curOp) { this.onRecordKey(key, mods); return; }
    if (this.curKind !== 'key' || this.capturing !== 'key' || this.capturingOp !== this.curOp) return;
    if (this.actionDialog) this.actionDialog.action.key = key;
    else this.builder.key = key;
    this.capturing = null;
    this.capturingOp = null;
    void this.stopAndResume({ id: this.curId ?? undefined });
  }

  private onRecordKey(key: string, mods?: string[] | null): void {
    const a = this.actionDialog?.action as KeyPressAction | undefined;
    if (!a) return;
    const r = recordEvent({ keys: sequenceOf(a), mods: this.recordMods }, key, mods);
    this.recordNote = r.ignored ? (r.stop ? 'Maximum keys reached.' : `Unsupported key ignored (${key}).`) : '';
    a.keys = r.keys;
    delete a.key;
    this.recordMods = r.mods;
    if (r.stop) {
      this.capturing = null;
      this.capturingOp = null;
      void this.stopAndResume({ id: this.curId ?? undefined });
    }
  }

  private onCaptureMouse(x: number, y: number): void {
    if (!this.sessionId || this.curKind !== 'mouse') return;
    const el = typeof document !== 'undefined' ? document.activeElement : null;
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA')) return;
    this.mouse = { x, y };
  }

  private async save(opts: { override?: boolean; ignore?: boolean } = {}): Promise<SaveResult> {
    if (!this.script || !this.sessionId) return 'failed';
    const sid = this.sessionId;
    const gen = this.gen;
    try {
      const r = await this.api.call('editor.save', {
        sessionId: sid, script: $state.snapshot(this.script),
        ...(opts.override ? { overrideConflicts: true } : {}), ...(opts.ignore ? { ignore_conflicts: true } : {}),
      });
      if (this.sessionId !== sid || !this.script) return 'failed';
      this.saveFailed = false;
      if (gen !== this.gen) {
        // Edited while the save was in flight: keep the newer draft, adopt only the persisted identity.
        this.script.id = r.script.id;
        return 'stale';
      }
      this.script = r.script;
      this.dirty = false;
      return 'ok';
    } catch (e) {
      if (this.sessionId !== sid) return 'failed';
      if (e instanceof ApiError) {
        if (gen !== this.gen) return 'stale'; // verdict on an outdated snapshot: re-save the newer draft
        if (e.code === 'HOTKEY_CONFLICT') { this.conflicts = e.body.conflicts ?? []; return 'conflict'; }
        if (e.code === 'NAME_EMPTY' || e.code === 'NAME_TAKEN') return 'name';
      }
      this.saveFailed = true;
      return 'failed';
    }
  }

  private async saveUntilClean(opts: { override?: boolean } = {}): Promise<SaveResult> {
    for (let i = 0; i < MAX_SAVE_PASSES; i++) {
      const r = await this.save(opts);
      if (r !== 'stale') return r;
    }
    return 'failed';
  }

  // A failed or invalid save never discards the draft: the user must explicitly choose to discard.
  // Conflicts wait for the user's override choice.
  close(): Promise<CloseResult> {
    if (this.closePromise) return this.closePromise;
    if (!this.sessionId) return Promise.resolve('noop');
    const p = this.doClose().then((r) => { if (r === 'conflict' || r === 'failed') this.reconcile(); return r; }).finally(() => { this.closePromise = null; });
    this.closePromise = p;
    return p;
  }

  private async doClose(): Promise<CloseResult> {
    this.closing = true;
    try {
      if (this.dirty) {
        const r = await this.saveUntilClean();
        const untouched = this.gen === 0 && this.script?.id === '' && !this.script.name.trim();
        if (!this.sessionId) return 'noop';
        if (r === 'conflict') return 'conflict';
        if (r === 'failed' || (r === 'name' && !untouched)) {
          this.discardPrompt = r === 'name' ? 'Script not saved: ' + this.indicator.text.replace('Not saved: ', '') + '.' : 'Saving the script failed.';
          return 'failed';
        }
      }
      await this.finish();
      return 'closed';
    } finally {
      this.closing = false;
    }
  }

  async resolveConflict(override: boolean): Promise<CloseResult> {
    this.conflicts = null;
    if (!override) { this.app.pendingRoute = null; this.reconcile(); }
    if (!override || !this.sessionId) return 'noop';
    this.closing = true;
    try {
      const r = await this.saveUntilClean({ override: true });
      if (!this.sessionId) return 'noop';
      if (r === 'ok') { await this.finish(); return 'closed'; }
      this.closing = false;
      if (r !== 'conflict') this.discardPrompt = r === 'name' ? 'Script not saved: ' + this.indicator.text.replace('Not saved: ', '') + '.' : 'Saving the script failed.';
      this.reconcile();
      return r === 'conflict' ? 'conflict' : 'failed';
    } finally {
      this.closing = false;
    }
  }

  keepEditing(): void {
    this.discardPrompt = null;
    this.app.pendingRoute = null;
    this.reconcile();
  }

  async discardAndClose(): Promise<void> {
    if (!this.sessionId || this.discardPrompt === null) return;
    this.closing = true;
    try {
      await this.finish();
    } finally {
      this.closing = false;
    }
  }

  // Native window close: autosave then release the session; conflicts cannot be prompted here.
  async flushForClose(): Promise<void> {
    if (this.closePromise) await this.closePromise.catch(() => {});
    if (!this.sessionId) return;
    this.closing = true;
    this.conflicts = null;
    try {
      if (this.dirty) await this.save({ ignore: true }); // parity with the old dialog's autosave on close
    } finally {
      await this.finish();
      this.closing = false;
    }
  }

  private async finish(): Promise<void> {
    const sessionId = this.sessionId;
    try {
      if (sessionId) await this.api.call('editor.close', { sessionId, autosave: false });
    } catch {
      // the host forces edit mode off on its side; nothing more to do here
    } finally {
      this.reset();
      this.app.route = this.app.pendingRoute ?? { name: 'scripts' };
      this.app.pendingRoute = null;
      await this.app.loadScripts().catch(() => {});
    }
  }

  private reset(): void {
    this.sessionId = null;
    this.script = null;
    this.catalog = [];
    this.labels = [];
    this.selected = -1;
    this.dirty = false;
    this.saveFailed = false;
    this.builder = { key: '', delay: 100, loop: 1 };
    this.capturing = null;
    this.curId = null;
    this.curKind = null;
    this.curOp = null;
    this.capturingOp = null;
    this.pendingStarts = 0;
    this.starting = false;
    this.buffered = [];
    this.needResume = false;
    this.conflicts = null;
    this.actionDialog = null;
    this.raw = null;
    this.discardPrompt = null;
    this.gen = 0;
  }

  async openRaw(): Promise<void> {
    if (!this.script) return;
    try {
      const r = await this.api.call('editor.toJSON', { script: $state.snapshot(this.script) });
      this.raw = { text: r.text, error: '' };
    } catch { this.app.error = 'Could not open the raw editor.'; }
  }

  async applyRaw(text: string): Promise<void> {
    if (!this.script) return;
    try {
      const r = await this.api.call('editor.fromJSON', { text });
      this.script = { ...r.script, id: this.script.id };
      this.raw = null;
      this.selected = -1;
      await this.changed();
    } catch {
      if (this.raw) this.raw.error = 'Invalid JSON';
    }
  }

  closeRaw(): void { this.raw = null; }
}
