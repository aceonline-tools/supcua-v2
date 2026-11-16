import { ApiError, type Client } from '../api/client';
import type { Action, ActionSpec, CaptureEndReason, HotkeyConflict, HotkeyField, Script } from '../api/types';
import type { AppStore } from './store.svelte';

export type CloseResult = 'closed' | 'conflict' | 'failed' | 'noop';
type SaveResult = 'ok' | 'conflict' | 'name' | 'failed';
export type Capturing = HotkeyField | 'key' | null;

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
  conflicts = $state<HotkeyConflict[] | null>(null);
  actionDialog = $state<ActionDialogState | null>(null);
  raw = $state<{ text: string; error: string } | null>(null);
  private closing = false;
  private closePromise: Promise<CloseResult> | null = null;
  private warned = false;
  // Host-side capture is single-mode (bridge/capture.go): Start ends the previous capture and emits
  // capture.ended{stopped}. We track what the host has active so those transition events are not
  // mistaken for a cancellation of the capture we just started.
  private activeKind: 'mouse' | 'hotkey' | 'key' | null = null;
  private expectedEnds = 0;
  private needResume = false;

  constructor(private api: Client, private app: AppStore) {
    api.on('capture.hotkey', (e) => this.onCaptureHotkey(e.field, e.hotkey));
    api.on('capture.key', (e) => this.onCaptureKey(e.key));
    api.on('capture.mouse', (e) => this.onCaptureMouse(e.x, e.y));
    api.on('capture.ended', (e) => this.onCaptureEnded(e.reason));
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
      this.activeKind = null;
      this.expectedEnds = 0;
      this.needResume = false;
      // A new script has never been saved: closing must persist it (autosave-on-close parity).
      this.dirty = id === null;
    } catch (e) {
      this.app.error = e instanceof ApiError ? (e.code === 'EDITOR_BUSY' ? 'Another editor is already open.' : e.message) : 'Failed to open editor';
      return;
    }
    this.app.route = { name: 'editor', id };
    await this.refreshLabels();
    await this.resumeMouse();
  }

  markDirty(): void {
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

  async insert(action: Action): Promise<void> {
    if (!this.script) return;
    const at = this.selected >= 0 && this.selected < this.script.actions.length ? this.selected + 1 : this.script.actions.length;
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
    if (this.capturing === 'key') this.cancelCapture();
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

  private async begin(kind: 'hotkey' | 'key', start: () => Promise<unknown>): Promise<void> {
    if (this.activeKind) this.expectedEnds++;
    this.activeKind = kind;
    try {
      await start();
    } catch {
      this.expectedEnds = 0;
      this.activeKind = null;
      this.capturing = null;
      await this.resumeMouse();
    }
  }

  async startCapture(field: HotkeyField): Promise<void> {
    const sessionId = this.sessionId;
    if (!sessionId) return;
    this.capturing = field;
    await this.begin('hotkey', () => this.api.call('capture.hotkey.start', { sessionId, field }));
  }

  async startKeyCapture(): Promise<void> {
    const sessionId = this.sessionId;
    if (!sessionId) return;
    this.capturing = 'key';
    await this.begin('key', () => this.api.call('capture.key.start', { sessionId }));
  }

  // Ends the current host capture (accepted, cancelled) and returns to mouse picking.
  private async stopCapture(): Promise<void> {
    if (!this.sessionId) return;
    await this.api.call('capture.stop', { sessionId: this.sessionId }).catch(() => {});
    await this.resumeMouse();
  }

  async cancelCapture(): Promise<void> {
    if (!this.capturing) return;
    this.capturing = null;
    await this.stopCapture();
  }

  private async resumeMouse(): Promise<void> {
    if (!this.sessionId || this.closing || this.activeKind) return;
    this.needResume = false;
    this.activeKind = 'mouse';
    try {
      await this.api.call('capture.mouse.start', { sessionId: this.sessionId });
    } catch {
      this.activeKind = null;
    }
  }

  onActivated(): void {
    if (this.needResume) void this.resumeMouse();
  }

  private onCaptureEnded(reason: CaptureEndReason): void {
    if (reason === 'stopped' && this.expectedEnds > 0) { this.expectedEnds--; return; }
    this.activeKind = null;
    this.expectedEnds = 0;
    this.capturing = null;
    if (!this.sessionId || reason === 'editorClosed') return;
    if (reason === 'sessionReset') { void this.onSessionReset(); return; }
    if (reason === 'blur') { this.needResume = true; return; }
    void this.resumeMouse();
  }

  private async onSessionReset(): Promise<void> {
    this.reset();
    this.app.route = { name: 'scripts' };
    this.app.error = 'The editor session was reset; unsaved changes were lost.';
    await this.app.loadScripts().catch(() => {});
  }

  clearHotkey(field: HotkeyField): void {
    if (!this.script) return;
    this.script[`${field}_hotkey` as const] = null;
    this.markDirty();
  }

  private onCaptureHotkey(field: HotkeyField, hotkey: { modifiers: string[]; key: string } | null): void {
    if (!this.script || this.capturing !== field) return;
    if (!hotkey || hotkey.key.toLowerCase() === 'esc' || hotkey.key.toLowerCase() === 'escape') { void this.cancelCapture(); return; }
    this.script[`${field}_hotkey` as const] = hotkey;
    this.capturing = null;
    this.markDirty();
    void this.stopCapture();
  }

  private onCaptureKey(key: string): void {
    if (this.capturing !== 'key') return;
    if (this.actionDialog) this.actionDialog.action.key = key;
    else this.builder.key = key;
    this.capturing = null;
    void this.stopCapture();
  }

  private onCaptureMouse(x: number, y: number): void {
    if (!this.sessionId) return;
    const el = typeof document !== 'undefined' ? document.activeElement : null;
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA')) return;
    this.mouse = { x, y };
  }

  private async save(override = false): Promise<SaveResult> {
    if (!this.script || !this.sessionId) return 'failed';
    try {
      const r = await this.api.call('editor.save', {
        sessionId: this.sessionId, script: $state.snapshot(this.script), ...(override ? { overrideConflicts: true } : {}),
      });
      this.script = r.script;
      this.dirty = false;
      this.saveFailed = false;
      return 'ok';
    } catch (e) {
      if (e instanceof ApiError) {
        if (e.code === 'HOTKEY_CONFLICT') { this.conflicts = e.body.conflicts ?? []; return 'conflict'; }
        if (e.code === 'NAME_EMPTY' || e.code === 'NAME_TAKEN') return 'name';
      }
      this.saveFailed = true;
      return 'failed';
    }
  }

  // Parity with autoSave(): a name problem silently skips the save and still closes.
  // Conflicts wait for the user; a hard save failure keeps the editor open once.
  close(): Promise<CloseResult> {
    if (this.closePromise) return this.closePromise;
    if (!this.sessionId) return Promise.resolve('noop');
    const p = this.doClose().finally(() => { this.closePromise = null; });
    this.closePromise = p;
    return p;
  }

  private async doClose(): Promise<CloseResult> {
    this.closing = true;
    try {
      if (this.dirty) {
        const r = await this.save();
        if (r === 'conflict') return 'conflict';
        if (r === 'failed' && !this.warned) { this.warned = true; return 'failed'; }
        if (r === 'name' && !(this.script?.id === '' && !this.script.name.trim())) this.app.error = 'Script not saved: ' + this.indicator.text.replace('Not saved: ', '') + '.';
      }
      await this.finish();
      return 'closed';
    } finally {
      this.closing = false;
    }
  }

  async resolveConflict(override: boolean): Promise<CloseResult> {
    this.conflicts = null;
    if (!override) this.app.pendingRoute = null;
    if (!override || !this.sessionId) return 'noop';
    this.closing = true;
    try {
      const r = await this.save(true);
      if (r === 'ok') { await this.finish(); return 'closed'; }
      return r === 'conflict' ? 'conflict' : 'failed';
    } finally {
      this.closing = false;
    }
  }

  // Native window close: autosave then release the session; conflicts cannot be prompted here.
  async flushForClose(): Promise<void> {
    if (this.closePromise) { await this.closePromise.catch(() => {}); return; }
    if (!this.sessionId) return;
    this.closing = true;
    try {
      if (this.dirty) await this.save();
      this.conflicts = null;
      await this.finish();
    } finally {
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
    this.conflicts = null;
    this.actionDialog = null;
    this.raw = null;
    this.warned = false;
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
