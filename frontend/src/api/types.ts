// Canonical source for DTOs that §9.3 leaves undefined (ScriptSummary, GroupDTO, SettingsDTO); all snake_case.
// Backend mirrors these with Go json tags; fixtures in ./fixtures back the contract test.

export type RunMode = 'once' | 'infinite';
export type HotkeyMode = 'toggle' | 'separate';
export type ThemeSetting = 'light' | 'dark' | 'system';

export interface OverlayDTO {
  enabled: boolean;
  visible: boolean;
  draggable: boolean;
  x: number;
  y: number;
}

export interface AutoActiveDTO {
  enabled: boolean;
  process_name: string;
}

export interface StateDTO {
  global_active: boolean;
  running_id: string;
  running_name: string;
  editor_open: boolean;
  overlay: OverlayDTO;
  theme: ThemeSetting;
  auto_active: AutoActiveDTO;
  mouse_locked: boolean;
  keyboard_locked: boolean;
}

export interface ScriptSummary {
  id: string;
  name: string;
  group: string;
  run_mode: RunMode;
  hotkey_mode: HotkeyMode;
  hotkey_label: string;
  enabled: boolean;
  running: boolean;
}

export interface GroupDTO {
  name: string;
  count: number;
  all_enabled: boolean;
  collapsed: boolean;
}

export interface ScriptList {
  rev: number;
  scripts: ScriptSummary[];
  groups: GroupDTO[];
}

export interface BootState {
  mode: 'main' | 'notLicensed';
  version: string;
  macs: string[];
  runtimeVersion: string;
}

// §9.4 config field names. Hotkeys are display strings on this surface.
export interface SettingsDTO {
  theme: ThemeSetting;
  global_hotkey: string;
  capture_hotkey: string;
  overlay_hotkey: string;
  overlay_enabled: boolean;
  key_press_duration_ms: number;
  scripts_dir: string;
  auto_active_enabled: boolean;
  auto_active_process_name: string;
  collapsed_groups: string[];
}

export type SettingsPatch = Partial<Omit<SettingsDTO, 'collapsed_groups'>>;

export interface SettingsUpdateResult {
  applied: string[];
  restartRequired: string[];
  state: StateDTO;
}

export interface Hotkey {
  modifiers: string[];
  key: string;
}

export interface Region {
  x: number;
  y: number;
  width: number;
  height: number;
}

// model/action.go json tags; zero-valued fields are omitted on the wire.
export interface Action {
  type: string;
  x?: number;
  y?: number;
  source?: string;
  offset_x?: number;
  offset_y?: number;
  key?: string;
  ms?: number;
  count?: number;
  actions?: Action[];
  image?: string;
  region?: Region;
  timeout_ms?: number;
  use_found_position?: boolean;
  then?: Action[];
  else?: Action[];
}

// model/script.go
export interface Script {
  id: string;
  name: string;
  enabled: boolean;
  group?: string;
  run_mode: RunMode;
  hotkey_mode: HotkeyMode;
  toggle_hotkey?: Hotkey | null;
  start_hotkey?: Hotkey | null;
  stop_hotkey?: Hotkey | null;
  actions: Action[];
}

export type HotkeyField = 'toggle' | 'start' | 'stop';
export type ActionGroup = 'mouse' | 'keyboard' | 'wait' | 'loop' | 'lock' | 'image';

// ASSUMED: param `name` equals the Action JSON tag it edits; kind is int|string|bool.
export interface ParamSpec {
  name: string;
  kind: 'int' | 'string' | 'bool';
  min?: number;
  max?: number;
  default?: number | string | boolean;
}

export interface ActionSpec {
  type: string;
  group: ActionGroup;
  params: ParamSpec[];
}

export interface EditorSession {
  sessionId: string;
  script: Script;
  catalog: ActionSpec[];
}

export interface HotkeyConflict {
  scriptId: string;
  name: string;
  field: 'toggle_hotkey' | 'start_hotkey' | 'stop_hotkey';
  hotkey: Hotkey;
}

export type CaptureEndReason = 'stopped' | 'blur' | 'timeout' | 'editorClosed';

export type ErrorCode =
  | 'BAD_REQUEST' | 'UNKNOWN_METHOD' | 'BUSY' | 'INTERNAL' | 'NOT_FOUND' | 'NAME_EMPTY'
  | 'NAME_TAKEN' | 'HOTKEY_CONFLICT' | 'HOTKEY_REGISTER_FAILED' | 'EDITOR_BUSY' | 'EDITOR_CLOSED'
  | 'SAVE_FAILED' | 'INVALID_SETTING' | 'UPDATE_FAILED' | 'NOT_LICENSED';

export interface ApiErrorBody {
  code: ErrorCode;
  message: string;
  field?: string;
  conflicts?: HotkeyConflict[];
}

export interface Methods {
  'app.getBootState': { params: undefined; result: BootState };
  'app.ready': { params: undefined; result: Record<string, never> };
  'state.get': { params: undefined; result: StateDTO };
  'app.setGlobalActive': { params: { active: boolean }; result: StateDTO };
  'app.exit': { params: undefined; result: Record<string, never> };
  'window.hide': { params: undefined; result: Record<string, never> };
  'scripts.list': { params: undefined; result: ScriptList };
  'scripts.setEnabled': { params: { id: string; enabled: boolean }; result: { rev: number } };
  'scripts.setGroupEnabled': { params: { group: string; enabled: boolean }; result: { rev: number; changed: number } };
  'scripts.setGroupCollapsed': { params: { group: string; collapsed: boolean }; result: Record<string, never> };
  'scripts.clone': { params: { id: string }; result: ScriptSummary };
  'scripts.delete': { params: { id: string }; result: { rev: number } };
  'editor.open': { params: { id: string | null }; result: EditorSession };
  'editor.save': { params: { sessionId: string; script: Script; overrideConflicts?: boolean }; result: { script: Script; rev: number } };
  'editor.close': { params: { sessionId: string; autosave: boolean }; result: { saved: boolean; rev: number } };
  'editor.describeActions': { params: { actions: Action[] }; result: string[] };
  'editor.toJSON': { params: { script: Script }; result: { text: string } };
  'editor.fromJSON': { params: { text: string }; result: { script: Script } };
  'capture.hotkey.start': { params: { sessionId: string; field: HotkeyField }; result: Record<string, never> };
  'capture.key.start': { params: { sessionId: string }; result: Record<string, never> };
  'capture.stop': { params: { sessionId: string }; result: Record<string, never> };
  'capture.mouse.start': { params: { sessionId: string }; result: Record<string, never> };
  'capture.mouse.stop': { params: { sessionId: string }; result: Record<string, never> };
  'clipboard.write': { params: { text: string }; result: Record<string, never> };
  'update.apply': { params: undefined; result: Record<string, never> };
  'update.skip': { params: undefined; result: Record<string, never> };
  'scripts.retrySave': { params: { id: string }; result: Record<string, never> };
  'settings.get': { params: undefined; result: SettingsDTO };
  'settings.update': { params: { patch: SettingsPatch }; result: SettingsUpdateResult };
  'overlay.set': { params: Partial<Pick<OverlayDTO, 'enabled' | 'visible' | 'draggable'>>; result: OverlayDTO };
  'autoactive.set': { params: { enabled?: boolean; processName?: string }; result: { enabled: boolean; processName: string } };
}

export type MethodName = keyof Methods;

export interface Events {
  state: StateDTO;
  'scripts.changed': { rev: number };
  'settings.changed': { fields: string[] };
  'save.failed': { scriptId: string; name: string; message: string };
  'capture.hotkey': { field: HotkeyField; hotkey: Hotkey | null; display: string };
  'capture.key': { key: string };
  'capture.mouse': { x: number; y: number };
  'capture.ended': { reason: CaptureEndReason };
  'update.available': { tag: string };
  'update.progress': { pct: number };
  'update.failed': { message: string };
  'nav.goto': { route: string };
  'app.closing': Record<string, never>;
}

export type EventName = keyof Events;
