import { ApiError, type Transport } from './client';
import { formatHotkey, parseHotkey } from '../lib/format';
import { OVERLAY_DEFAULTS, clampOverlayStyle, validateOverlayStyle } from '../lib/overlayStyle';
import type {
  Action, ActionSpec, BootState, GroupDTO, HotkeyConflict, ParamSpec, RunMode, Script, ScriptList,
  ScriptSummary, SettingsDTO, StateDTO, OverlayStyleDTO,
  UpdateCheckResult,
} from './types';

export interface MockSeed {
  scripts: ScriptSummary[];
  settings: SettingsDTO;
  state: StateDTO;
  boot: BootState;
  collapsed: string[];
  updateTag?: string;
  /** What update.check answers (default: up to date) and after how long. */
  updateCheck?: Omit<UpdateCheckResult, 'current'>;
  updateCheckDelayMs?: number;
}

const s = (id: string, name: string, group: string, hotkey: string, run: RunMode, enabled = true): ScriptSummary => ({
  id, name, group, run_mode: run, hotkey_mode: 'toggle', hotkey_label: hotkey, enabled, running: false,
});

export function defaultSeed(): MockSeed {
  return {
    scripts: [
      s('s1', 'Defense buff', 'Buffs', 'F5', 'once'),
      s('s2', 'Shield refresh', 'Buffs', 'F6 → F7', 'infinite'),
      s('s3', 'Auto attack', 'Farming', 'F1', 'infinite'),
      s('s4', 'Collect loot', 'Farming', 'Ctrl + Shift + 1', 'once'),
      s('s5', 'Repair cycle', 'Farming', 'F3', 'once', false),
    ],
    settings: {
      theme: 'dark', global_hotkey: 'RCtrl', capture_hotkey: 'P', overlay_hotkey: 'Ctrl + O',
      overlay_enabled: true, key_press_duration_ms: 10, scripts_dir: './scripts',
      auto_active_enabled: true, auto_active_process_name: 'game.exe', collapsed_groups: [], sidebar_collapsed: false,
      overlay_style: { ...OVERLAY_DEFAULTS },
    },
    state: {
      global_active: true, running_id: '', running_name: '', editor_open: false,
      overlay: { enabled: true, visible: true, draggable: false, x: 40, y: 40 },
      theme: 'dark', mouse_locked: false, keyboard_locked: false, auto_active: { enabled: true, process_name: 'game.exe' },
    },
    boot: { mode: 'main', version: 'v1.0.28', macs: ['02:00:00:00:00:01'], runtimeVersion: '130.0.2849.80' },
    collapsed: [],
  };
}

// Demo knobs for browser-only dev and screenshots: ?mock=license|update&theme=light
export function seedFromQuery(search: string): MockSeed {
  const q = new URLSearchParams(search);
  const seed = defaultSeed();
  const theme = q.get('theme');
  if (theme === 'light' || theme === 'dark' || theme === 'system') { seed.state.theme = theme; seed.settings.theme = theme; }
  if (q.get('mock') === 'license') seed.boot.mode = 'notLicensed';
  if (q.get('mock') === 'update') seed.updateTag = 'v1.0.29';
  const uc = q.get('updcheck');
  if (uc === 'available') seed.updateCheck = { status: 'available', tag: '1.0.29' };
  else if (uc === 'offline' || uc === 'rate_limited' || uc === 'other') seed.updateCheck = { status: 'error', error: uc };
  const ud = Number(q.get('upddelay'));
  if (Number.isFinite(ud) && ud > 0) seed.updateCheckDelayMs = Math.min(ud, 60000);
  return seed;
}

const P = (name: string, kind: ParamSpec['kind'], min?: number, max?: number, def?: ParamSpec['default']): ParamSpec => ({ name, kind, min, max, default: def });
const spec = (type: string, group: ActionSpec['group'], params: ParamSpec[] = []): ActionSpec => ({ type, group, params });

export const MOCK_CATALOG: ActionSpec[] = [
  spec('mouse_move', 'mouse', [P('x', 'int', 0, 10000, 0), P('y', 'int', 0, 10000, 0)]),
  ...['mouse_left_click', 'mouse_right_click', 'mouse_left_double', 'mouse_right_double', 'mouse_left_down', 'mouse_left_up',
    'mouse_right_down', 'mouse_right_up', 'mouse_store_pos', 'mouse_move_stored'].map((t) => spec(t, 'mouse')),
  spec('key_press', 'keyboard', [P('key', 'string'), P('keys', 'keys'), P('key_delay_ms', 'int', 0, 5000, 50)]),
  ...['key_down', 'key_up'].map((t) => spec(t, 'keyboard', [P('key', 'string')])),
  spec('delay', 'wait', [P('ms', 'int', 1, 600000, 100)]),
  spec('loop', 'loop', [P('count', 'int', 0, 100000, 1)]),
  spec('loop_end', 'loop'),
  spec('wait_for_image', 'image', [P('timeout_ms', 'int', 0, 600000, 5000), P('use_found_position', 'bool')]),
  spec('if_image', 'image', [P('use_found_position', 'bool')]),
  ...['lock_mouse', 'unlock_mouse', 'lock_keyboard', 'unlock_keyboard'].map((t) => spec(t, 'lock')),
];

const LABELS: Record<string, string> = {
  mouse_store_pos: 'Mouse Store Position', mouse_move_stored: 'Mouse Move to Stored', mouse_left_click: 'Mouse Left Click',
  mouse_right_click: 'Mouse Right Click', mouse_left_double: 'Mouse Left Double Click', mouse_right_double: 'Mouse Right Double Click',
  mouse_left_down: 'Mouse Left Down', mouse_left_up: 'Mouse Left Up', mouse_right_down: 'Mouse Right Down', mouse_right_up: 'Mouse Right Up',
  lock_mouse: 'Lock Mouse', unlock_mouse: 'Unlock Mouse', lock_keyboard: 'Lock Keyboard', unlock_keyboard: 'Unlock Keyboard',
  loop_end: 'End Loop',
};

import { describeKeyPress } from '../lib/keySequence';

function describe(a: Action): string {
  switch (a.type) {
    case 'mouse_move': return `Mouse Move → (${a.x ?? 0}, ${a.y ?? 0})`;
    case 'key_press': return describeKeyPress(a);
    case 'key_down': return `Key Down: ${a.key ?? ''}`;
    case 'key_up': return `Key Up: ${a.key ?? ''}`;
    case 'delay': return `Delay: ${a.ms ?? 0}ms`;
    case 'loop': return `Loop ×${a.count || '∞'} (${a.actions?.length ?? 0} actions)`;
    default: return LABELS[a.type] ?? a.type;
  }
}

const SAMPLE_ACTIONS: Action[] = [
  { type: 'mouse_store_pos' }, { type: 'mouse_move', x: 640, y: 360 }, { type: 'mouse_left_click' },
  { type: 'key_press', key: '1' }, { type: 'delay', ms: 100 }, { type: 'mouse_move_stored' },
];

const FIELDS = [['toggle', 'toggle_hotkey'], ['start', 'start_hotkey'], ['stop', 'stop_hotkey']] as const;
const RESTART_ONLY = new Set(['key_press_duration_ms', 'scripts_dir']);

function labelOf(sc: Script): string {
  return sc.hotkey_mode === 'separate' ? `${formatHotkey(sc.start_hotkey)} → ${formatHotkey(sc.stop_hotkey)}` : formatHotkey(sc.toggle_hotkey);
}

export class MockBackend implements Transport {
  readonly calls: { method: string; params: unknown }[] = [];
  failSaveFor = new Set<string>();
  private seed: MockSeed;
  private bodies = new Map<string, Script>();
  private session: string | null = null;
  private pendingId: string | null | undefined;
  private gen = 0;
  private sessionRev = 0;
  private lastSaved = 0;
  readonly checkpoints: { revision: number; applied: boolean }[] = [];
  private activeCap: { id: number; method: string } | null = null;
  private capId = 0;
  private settingsCap: { id: number; field: string } | null = null;
  /** Mutes the host's global hotkeys while a Settings recording runs (host: hotkey.Manager.SetSuspended). */
  hotkeysMuted = false;
  /** Hotkeys another app owns: settings.update fails with HOTKEY_REGISTER_FAILED for these. */
  readonly takenByOtherApp = new Set(['Ctrl + Alt + F12']);
  private rev = 1;
  private nextId = 100;
  private emit: (name: string, payload: unknown) => void = () => {};

  constructor(seed: MockSeed = defaultSeed()) {
    this.seed = structuredClone(seed);
    if (typeof window !== 'undefined') {
      // No low-level hook here: the browser's own keys stand in for it while a Settings recording runs
      // (the simulator shell forwards its iframes' keys to hostKey instead, see sim/web/main.ts).
      const feed = (down: boolean) => (e: KeyboardEvent) => {
        if (!this.settingsCap) return;
        e.preventDefault();
        this.hostKey(e.code, down, { ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey }, e.key);
      };
      window.addEventListener('keydown', feed(true), true);
      window.addEventListener('keyup', feed(false), true);
    }
  }

  /** Dev preview/tests: gives script `id` n sample actions. */
  padActions(id: string, n: number): void {
    const b = this.body(id);
    b.actions = Array.from({ length: n }, (_, i) => structuredClone(SAMPLE_ACTIONS[i % SAMPLE_ACTIONS.length]));
  }

  get currentGen(): number {
    return this.gen;
  }

  /** Host-side rollback of an editor window that never reached editor.open (create/embed failure). */
  abortOpen(): void {
    this.pendingId = undefined;
    this.session = null;
    this.seed.state.editor_open = false;
  }

  private endSettingsCap(): void {
    this.settingsCap = null;
    this.hotkeysMuted = false;
  }

  get currentCaptureId(): number {
    return this.activeCap?.id ?? 0;
  }

  subscribe(handler: (name: string, payload: unknown) => void): void {
    this.emit = handler;
  }

  /** Emits a host key-down for the active capture; mods null mimics a host that reports no modifier state. */
  pushKey(key: string, mods: string[] | null = []): void {
    this.push('capture.key', { capture_id: this.currentCaptureId, key, mods });
  }

  /** Host keyboard feed of the Settings recorder: a modifier released alone is a tap, anything else a combo. */
  private tapPending: string | null = null;
  private tapHeld = new Set<string>();
  private static readonly SIDED: Record<string, string> = {
    ControlLeft: 'LCtrl', ControlRight: 'RCtrl', ShiftLeft: 'LShift', ShiftRight: 'RShift', AltLeft: 'LAlt', AltRight: 'RAlt',
  };

  hostKey(code: string, down: boolean, mods: { ctrl: boolean; alt: boolean; shift: boolean }, key = ''): void {
    const cap = this.settingsCap;
    if (!cap) return;
    const sided = MockBackend.SIDED[code];
    if (sided) {
      if (down) {
        if (!this.tapHeld.has(code)) this.tapPending = this.tapHeld.size === 0 ? code : null;
        this.tapHeld.add(code);
        return;
      }
      this.tapHeld.delete(code);
      if (this.tapPending === code) {
        this.tapPending = null;
        this.push('settings.capture.hotkey', { capture_id: cap.id, field: cap.field, hotkey: { modifiers: [], key: sided.toLowerCase() }, display: sided });
      }
      return;
    }
    if (!down) { this.tapHeld.delete(code); return; }
    this.tapHeld.add(code);
    this.tapPending = null;
    const name = key.length === 1 ? key.toUpperCase() : /^F\d+$/.test(key) ? key : key === 'Escape' ? 'Escape' : key;
    const modifiers = [mods.ctrl && 'ctrl', mods.alt && 'alt', mods.shift && 'shift'].filter(Boolean) as string[];
    const display = [...modifiers.map((m) => m[0].toUpperCase() + m.slice(1)), name === 'Escape' ? 'Esc' : name].join(' + ');
    this.push('settings.capture.hotkey', { capture_id: cap.id, field: cap.field, hotkey: { modifiers, key: name.toLowerCase() === 'escape' ? 'escape' : name.toLowerCase() }, display });
  }

  push(name: string, payload: unknown): void {
    if (name === 'settings.capture.ended' && (payload as { capture_id?: number }).capture_id === this.settingsCap?.id) this.endSettingsCap();
    if (name === 'capture.ended' && (payload as { capture_id?: number }).capture_id === this.activeCap?.id) this.activeCap = null; // host semantics
    this.emit(name, payload);
  }

  async call(method: string, params: unknown): Promise<unknown> {
    this.calls.push({ method, params: structuredClone(params) });
    await Promise.resolve();
    if (method === 'update.check' && this.seed.updateCheckDelayMs) await new Promise((r) => setTimeout(r, this.seed.updateCheckDelayMs));
    return structuredClone(this.handle(method, (params ?? {}) as Record<string, any>));
  }

  private body(id: string): Script {
    let b = this.bodies.get(id);
    if (!b) {
      const sm = this.find(id);
      b = { id, name: sm.name, enabled: sm.enabled, group: sm.group || undefined, run_mode: sm.run_mode, hotkey_mode: sm.hotkey_mode, actions: structuredClone(SAMPLE_ACTIONS) };
      const [a, z] = sm.hotkey_label.split(' → ');
      if (sm.hotkey_mode === 'separate') { b.start_hotkey = parseHotkey(a); b.stop_hotkey = parseHotkey(z ?? ''); }
      else b.toggle_hotkey = parseHotkey(a);
      this.bodies.set(id, b);
    }
    return b;
  }

  private list(): ScriptList {
    const scripts = [...this.seed.scripts].sort((a, b) =>
      (a.group === '' ? 0 : 1) - (b.group === '' ? 0 : 1) ||
      a.group.localeCompare(b.group, undefined, { sensitivity: 'base' }) ||
      a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
    const names = [...new Set(scripts.filter((x) => x.group !== '').map((x) => x.group))];
    const groups: GroupDTO[] = names.map((name) => {
      const members = scripts.filter((x) => x.group === name);
      return { name, count: members.length, all_enabled: members.every((m) => m.enabled), collapsed: this.seed.collapsed.includes(name) };
    });
    return { rev: this.rev, scripts, groups };
  }

  private find(id: string): ScriptSummary {
    const found = this.seed.scripts.find((x) => x.id === id);
    if (!found) throw new ApiError({ code: 'NOT_FOUND', message: 'Not found' });
    return found;
  }

  private persist(ids: string[], names: string[]): void {
    const bad = ids.findIndex((id) => this.failSaveFor.has(id));
    if (bad >= 0) {
      const id = ids[bad];
      setTimeout(() => this.emit('save.failed', { scriptId: id, name: names[bad], message: 'Could not write script file' }), 0);
    }
  }

  private requireSession(id: unknown): void {
    if (!this.session || id !== this.session) throw new ApiError({ code: 'EDITOR_CLOSED', message: 'Editor closed' });
  }

  private conflicts(sc: Script): HotkeyConflict[] {
    const out: HotkeyConflict[] = [];
    for (const other of this.seed.scripts) {
      if (other.id === sc.id) continue;
      const ob = this.body(other.id);
      for (const [, f] of FIELDS) {
        for (const [, mf] of FIELDS) {
          const mine = sc[mf]; const theirs = ob[f];
          if (mine && theirs && formatHotkey(mine) === formatHotkey(theirs)) out.push({ scriptId: other.id, name: other.name, field: f, hotkey: theirs });
        }
      }
    }
    return out;
  }

  private handle(method: string, p: Record<string, any>): unknown {
    const st = this.seed;
    switch (method) {
      case 'app.getBootState': return st.boot;
      case 'app.ready':
        if (st.updateTag) { const tag = st.updateTag; setTimeout(() => this.emit('update.available', { tag }), 0); }
        return {};
      case 'state.get': return st.state;
      case 'app.setGlobalActive': st.state.global_active = !!p.active; return st.state;
      case 'app.closingReady': return {};
      case 'app.exit': case 'window.hide': case 'window.show': return {};
      case 'scripts.list': return this.list();
      case 'scripts.setEnabled': {
        const sc = this.find(p.id);
        sc.enabled = !!p.enabled;
        this.persist([sc.id], [sc.name]);
        return { rev: ++this.rev };
      }
      case 'scripts.setGroupEnabled': {
        const members = st.scripts.filter((x) => x.group === p.group && x.enabled !== !!p.enabled);
        members.forEach((m) => (m.enabled = !!p.enabled));
        this.persist(members.map((m) => m.id), members.map((m) => m.name));
        return { rev: ++this.rev, changed: members.length };
      }
      case 'scripts.setGroupCollapsed': {
        const set = new Set(st.collapsed);
        if (p.collapsed) set.add(p.group); else set.delete(p.group);
        st.collapsed = [...set];
        st.settings.collapsed_groups = st.collapsed;
        return {};
      }
      case 'settings.setSidebarCollapsed': {
        st.settings.sidebar_collapsed = !!p.collapsed;
        return {};
      }
      case 'scripts.clone': {
        const src = this.find(p.id);
        let name = `${src.name} (copy)`;
        for (let n = 2; st.scripts.some((x) => x.name.toLowerCase() === name.toLowerCase()); n++) name = `${src.name} (copy ${n})`;
        const copy: ScriptSummary = { ...src, id: `s${this.nextId++}`, name, running: false };
        st.scripts.push(copy);
        this.rev++;
        return copy;
      }
      case 'scripts.delete': {
        this.find(p.id);
        st.scripts = st.scripts.filter((x) => x.id !== p.id);
        this.bodies.delete(p.id);
        return { rev: ++this.rev };
      }
      case 'scripts.retrySave': { this.find(p.id); this.failSaveFor.delete(p.id); return {}; }
      case 'editor.openWindow': {
        if (this.session || this.pendingId !== undefined) return { status: 'focused', gen: this.gen };
        this.pendingId = (p.id as string | undefined) ?? null;
        return { status: 'opening', gen: ++this.gen };
      }
      case 'editor.windowReady': setTimeout(() => this.emit('editor.measureRequest', { token: 1 }), 0); return {};
      case 'editor.open': {
        if (this.session) throw new ApiError({ code: 'EDITOR_BUSY', message: 'Editor busy' });
        const id = this.pendingId !== undefined ? this.pendingId : (p.id ?? null);
        this.pendingId = undefined;
        const isNew = id === null;
        const script: Script = id
          ? this.body(id)
          : { id: `s${this.nextId++}`, name: 'New Script', enabled: true, run_mode: 'once', hotkey_mode: 'toggle', toggle_hotkey: null, actions: [] };
        this.session = `e${this.nextId++}`;
        this.sessionRev = this.lastSaved = 0;
        this.activeCap = null;
        st.state.editor_open = true;
        const groups = [...new Set(st.scripts.map((x) => x.group).filter(Boolean))].sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase()));
        return { sessionId: this.session, script, catalog: MOCK_CATALOG, revision: 0, isNew, groups };
      }
      case 'editor.measured': return { next: 'show' };
      case 'editor.measureAck': return {};
      case 'editor.checkpoint': {
        this.requireSession(p.sessionId);
        const applied = p.revision > Math.max(this.sessionRev, this.lastSaved);
        if (applied) this.sessionRev = p.revision;
        this.checkpoints.push({ revision: p.revision, applied });
        return { applied, revision: p.revision };
      }
      case 'editor.guardActive': this.requireSession(p.sessionId); return { leaseMs: 3000 };
      case 'editor.requestClose': {
        if (this.session) setTimeout(() => this.emit('editor.closeRequested', { gen: this.gen, sessionId: this.session ?? undefined, reason: 'user' }), 0);
        return {};
      }
      case 'editor.closeDecision': {
        this.requireSession(p.sessionId);
        if (p.decision === 'cancel') return {};
        this.session = null;
        st.state.editor_open = false;
        if (this.activeCap) { const id = this.activeCap.id; this.activeCap = null; this.emit('capture.ended', { reason: 'editorClosed', capture_id: id }); }
        return {};
      }
      case 'editor.save': {
        this.requireSession(p.sessionId);
        const sc = p.script as Script;
        const name = sc.name.trim();
        if (!name) throw new ApiError({ code: 'NAME_EMPTY', message: 'Name required' });
        if (st.scripts.some((x) => x.id !== sc.id && x.name.toLowerCase() === name.toLowerCase())) throw new ApiError({ code: 'NAME_TAKEN', message: 'Name taken' });
        const conflicts = this.conflicts(sc);
        if (conflicts.length && !p.overrideConflicts && !p.ignore_conflicts) throw new ApiError({ code: 'HOTKEY_CONFLICT', message: 'Hotkey conflict', conflicts });
        for (const c of p.overrideConflicts ? conflicts : []) {
          const ob = this.body(c.scriptId);
          ob[c.field] = null;
          this.find(c.scriptId).hotkey_label = labelOf(ob);
        }
        const id = sc.id || `s${this.nextId++}`;
        if (this.failSaveFor.has(id)) throw new ApiError({ code: 'SAVE_FAILED', message: 'Save failed' });
        const saved: Script = { ...sc, name, id };
        this.bodies.set(id, structuredClone(saved));
        const summary: ScriptSummary = { id, name, group: sc.group ?? '', run_mode: sc.run_mode, hotkey_mode: sc.hotkey_mode, hotkey_label: labelOf(sc), enabled: sc.enabled, running: false };
        const at = st.scripts.findIndex((x) => x.id === id);
        if (at >= 0) st.scripts[at] = summary; else st.scripts.push(summary);
        const savedRev = (p.snapshotRev as number | undefined) ?? this.sessionRev + 1;
        this.lastSaved = Math.max(this.lastSaved, savedRev);
        this.sessionRev = Math.max(this.sessionRev, savedRev);
        setTimeout(() => this.emit('scripts.changed', { rev: this.rev }), 0);
        return { script: saved, rev: ++this.rev, savedRev };
      }
      case 'editor.describeActions': return (p.actions as Action[]).map(describe);
      case 'editor.toJSON': return { text: JSON.stringify(p.script, null, 2) };
      case 'editor.fromJSON': {
        try { return { script: JSON.parse(p.text) }; } catch { throw new ApiError({ code: 'BAD_REQUEST', message: 'Invalid JSON' }); }
      }
      case 'capture.hotkey.start': case 'capture.key.start': case 'capture.mouse.start': {
        this.requireSession(p.sessionId);
        // bridge/capture.go Start: ends the previous capture first and emits capture.ended{stopped}
        if (this.activeCap) this.emit('capture.ended', { reason: 'stopped', capture_id: this.activeCap.id });
        this.activeCap = { id: ++this.capId, method };
        return { capture_id: this.activeCap.id };
      }
      case 'settings.capture.start': {
        if (this.session) throw new ApiError({ code: 'EDITOR_BUSY', message: 'an editor is already open' });
        if (!['global_hotkey', 'overlay_hotkey', 'capture_hotkey'].includes(p.field)) throw new ApiError({ code: 'BAD_REQUEST', message: 'invalid request' });
        if (this.settingsCap) this.emit('settings.capture.ended', { reason: 'stopped', capture_id: this.settingsCap.id });
        this.tapPending = null; this.tapHeld.clear();
        this.settingsCap = { id: ++this.capId, field: p.field };
        this.hotkeysMuted = true;
        return { capture_id: this.settingsCap.id };
      }
      case 'settings.capture.stop': {
        const cur = this.settingsCap;
        if (cur && (p.capture_id === undefined || p.capture_id === cur.id)) { this.endSettingsCap(); this.emit('settings.capture.ended', { reason: 'stopped', capture_id: cur.id }); }
        return {};
      }
      case 'capture.stop': case 'capture.mouse.stop': {
        this.requireSession(p.sessionId);
        const cur = this.activeCap;
        if (cur && (p.capture_id === undefined || p.capture_id === cur.id)) { this.activeCap = null; this.emit('capture.ended', { reason: 'stopped', capture_id: cur.id }); }
        return {};
      }
      case 'clipboard.write': case 'update.apply': case 'update.skip': return {};
      case 'update.check': {
        const cur = st.boot.version;
        const sc = st.updateCheck ?? { status: 'up_to_date' as const };
        return { ...sc, current: cur };
      }
      case 'settings.get': return { ...st.settings, collapsed_groups: st.collapsed };
      case 'settings.update': {
        const patch = p.patch as Partial<SettingsDTO>;
        for (const [k, v] of Object.entries(patch)) {
          if (k === 'key_press_duration_ms' && !(Number.isInteger(v) && (v as number) > 0 && (v as number) <= 1000))
            throw new ApiError({ code: 'INVALID_SETTING', message: 'Invalid setting', field: k });
          if (k === 'scripts_dir' && !String(v).trim())
            throw new ApiError({ code: 'INVALID_SETTING', message: 'Invalid setting', field: k });
          if ((k === 'global_hotkey' || k === 'capture_hotkey' || k === 'overlay_hotkey') && !String(v).trim())
            throw new ApiError({ code: 'INVALID_SETTING', message: 'Invalid setting', field: k });
        }
        const hk = ['global_hotkey', 'overlay_hotkey', 'capture_hotkey'] as const;
        for (const k of hk) {
          const v = patch[k];
          if (v === undefined) continue;
          if (!/^((Ctrl|Alt|Shift) \+ )*[A-Za-z0-9;=,./`\[\]\\'-]+$/.test(v) || (v.length > 1 && !/^(F\d{1,2}|[LR](Ctrl|Shift|Alt)|Esc|Space|Enter|Tab|[A-Za-z]+)$/.test(v.split(' + ').pop() ?? '')))
            throw new ApiError({ code: 'INVALID_SETTING', message: 'Invalid setting', field: k });
          const clash = hk.find((o) => o !== k && (patch[o] ?? st.settings[o]).toLowerCase() === v.toLowerCase());
          if (clash) throw new ApiError({ code: 'INVALID_SETTING', message: 'Invalid setting', field: k });
          if (this.takenByOtherApp.has(v)) throw new ApiError({ code: 'HOTKEY_REGISTER_FAILED', message: 'could not register hotkey' });
        }
        Object.assign(st.settings, patch);
        if (patch.theme) st.state.theme = patch.theme;
        if (patch.overlay_enabled !== undefined) st.state.overlay.enabled = patch.overlay_enabled;
        const keys = Object.keys(patch);
        return { applied: keys.filter((k) => !RESTART_ONLY.has(k)), restartRequired: keys.filter((k) => RESTART_ONLY.has(k)), state: st.state };
      }
      case 'overlay.set': {
        for (const k of ['enabled', 'visible', 'draggable'] as const) if (typeof p[k] === 'boolean') st.state.overlay[k] = p[k];
        st.settings.overlay_enabled = st.state.overlay.enabled;
        return st.state.overlay;
      }
      case 'overlay.setStyle': {
        const v = validateOverlayStyle(p as unknown as OverlayStyleDTO);
        if (v) throw new ApiError({ code: 'INVALID_SETTING', message: 'Invalid setting', field: v });
        const eff = clampOverlayStyle(p as unknown as OverlayStyleDTO);
        st.settings.overlay_style = eff;
        st.state.overlay.x = eff.x; st.state.overlay.y = eff.y;
        this.emit('settings.changed', { fields: ['overlay_style'] });
        return eff;
      }
      case 'overlay.resetStyle': {
        st.settings.overlay_style = { ...OVERLAY_DEFAULTS };
        st.state.overlay.x = OVERLAY_DEFAULTS.x; st.state.overlay.y = OVERLAY_DEFAULTS.y;
        this.emit('settings.changed', { fields: ['overlay_style'] });
        return st.settings.overlay_style;
      }
      case 'autoactive.set': {
        if (typeof p.enabled === 'boolean') st.state.auto_active.enabled = p.enabled;
        if (typeof p.processName === 'string') st.state.auto_active.process_name = p.processName;
        st.settings.auto_active_enabled = st.state.auto_active.enabled;
        st.settings.auto_active_process_name = st.state.auto_active.process_name;
        return { enabled: st.state.auto_active.enabled, processName: st.state.auto_active.process_name };
      }
      default:
        throw new ApiError({ code: 'UNKNOWN_METHOD', message: 'Unknown method' });
    }
  }
}
