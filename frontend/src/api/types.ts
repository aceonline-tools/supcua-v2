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

// Overlay look; x/y are the top-left in virtual-screen pixels. text_color "" = status colours.
export interface OverlayStyleDTO {
  radius: number;
  background: string;
  text_color: string;
  background_opacity: number;
  opacity: number;
  width: number;
  height: number;
  x: number;
  y: number;
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
  sidebar_collapsed: boolean;
  overlay_style: OverlayStyleDTO;
}

export type SettingsPatch = Partial<Omit<SettingsDTO, 'collapsed_groups' | 'sidebar_collapsed' | 'overlay_style'>>;

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

// model/action.go KeyStroke: one key of a key_press sequence, pressed with its modifiers.
export interface KeyStroke {
  key: string;
  mods?: string[];
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
  keys?: KeyStroke[];
  key_delay_ms?: number;
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
export type SettingsHotkeyField = 'global_hotkey' | 'overlay_hotkey' | 'capture_hotkey';
export type ActionGroup = 'mouse' | 'keyboard' | 'wait' | 'loop' | 'lock' | 'image';

// ASSUMED: param `name` equals the Action JSON tag it edits; kind is int|string|bool.
export interface ParamSpec {
  name: string;
  kind: 'int' | 'string' | 'bool' | 'keys';
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
  revision?: number;
  isNew?: boolean;
  groups?: string[];
}

export type CloseDecision = 'saved' | 'discard' | 'cancel';
export type MeasureReply = { next: 'show' | 'remeasure'; token?: number; cssW?: number };

export interface HotkeyConflict {
  scriptId: string;
  name: string;
  field: 'toggle_hotkey' | 'start_hotkey' | 'stop_hotkey';
  hotkey: Hotkey;
}

export type CaptureEndReason = 'stopped' | 'blur' | 'timeout' | 'editorClosed';

export interface CaptureStarted {
  capture_id: number;
}

export type ErrorCode =
  | 'BAD_REQUEST' | 'UNKNOWN_METHOD' | 'BUSY' | 'INTERNAL' | 'NOT_FOUND' | 'NAME_EMPTY'
  | 'NAME_TAKEN' | 'HOTKEY_CONFLICT' | 'HOTKEY_REGISTER_FAILED' | 'EDITOR_BUSY' | 'EDITOR_CLOSED'
  | 'SAVE_FAILED' | 'INVALID_SETTING' | 'UPDATE_FAILED' | 'NOT_LICENSED'
  | 'FORBIDDEN_ROLE' | 'STALE_GEN' | 'SESSION_CLOSING' | 'DRAFT_TOO_LARGE' | 'SHUTTING_DOWN' | 'EDITOR_WINDOW_BUSY';

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
  'app.closingReady': { params: undefined; result: Record<string, never> };
  'app.exit': { params: undefined; result: Record<string, never> };
  'window.hide': { params: undefined; result: Record<string, never> };
  'scripts.list': { params: undefined; result: ScriptList };
  'scripts.setEnabled': { params: { id: string; enabled: boolean }; result: { rev: number } };
  'scripts.setGroupEnabled': { params: { group: string; enabled: boolean }; result: { rev: number; changed: number } };
  'scripts.setGroupCollapsed': { params: { group: string; collapsed: boolean }; result: Record<string, never> };
  'settings.setSidebarCollapsed': { params: { collapsed: boolean }; result: Record<string, never> };
  'scripts.clone': { params: { id: string }; result: ScriptSummary };
  'scripts.delete': { params: { id: string }; result: { rev: number } };
  'editor.openWindow': { params: { id?: string }; result: { status: 'opening' | 'focused'; gen: number } };
  'editor.windowReady': { params: undefined; result: Record<string, never> };
  'editor.open': { params: { id: string | null }; result: EditorSession };
  'editor.measured': { params: { token: number; cssW: number; cssH: number; dpr: number }; result: MeasureReply };
  'editor.measureAck': { params: { token: number }; result: Record<string, never> };
  'editor.checkpoint': { params: { sessionId: string; revision: number; script: Script }; result: { applied: boolean; revision: number } };
  'editor.guardActive': { params: { sessionId: string }; result: { leaseMs: number } };
  'editor.closeDecision': { params: { sessionId: string; decision: CloseDecision }; result: Record<string, never> };
  'editor.requestClose': { params: undefined; result: Record<string, never> };
  'editor.save': { params: { sessionId: string; script: Script; snapshotRev?: number; overrideConflicts?: boolean; ignore_conflicts?: boolean }; result: { script: Script; rev: number; savedRev?: number } };
  'editor.describeActions': { params: { actions: Action[] }; result: string[] };
  'editor.toJSON': { params: { script: Script }; result: { text: string } };
  'editor.fromJSON': { params: { text: string }; result: { script: Script } };
  'settings.capture.start': { params: { field: SettingsHotkeyField }; result: CaptureStarted };
  'settings.capture.stop': { params: { capture_id?: number }; result: Record<string, never> };
  'capture.hotkey.start': { params: { sessionId: string; field: HotkeyField }; result: CaptureStarted };
  'capture.key.start': { params: { sessionId: string }; result: CaptureStarted };
  'capture.stop': { params: { sessionId: string; capture_id?: number }; result: Record<string, never> };
  'capture.mouse.start': { params: { sessionId: string }; result: CaptureStarted };
  'capture.mouse.stop': { params: { sessionId: string }; result: Record<string, never> };
  'clipboard.write': { params: { text: string }; result: Record<string, never> };
  'update.apply': { params: undefined; result: Record<string, never> };
  'update.skip': { params: undefined; result: Record<string, never> };
  'scripts.retrySave': { params: { id: string }; result: Record<string, never> };
  'settings.get': { params: undefined; result: SettingsDTO };
  'settings.update': { params: { patch: SettingsPatch }; result: SettingsUpdateResult };
  'overlay.set': { params: Partial<Pick<OverlayDTO, 'enabled' | 'visible' | 'draggable'>>; result: OverlayDTO };
  'overlay.setStyle': { params: OverlayStyleDTO; result: OverlayStyleDTO };
  'overlay.resetStyle': { params: undefined; result: OverlayStyleDTO };
  'autoactive.set': { params: { enabled?: boolean; processName?: string }; result: { enabled: boolean; processName: string } };
}

export type MethodName = keyof Methods;

export interface Events {
  state: StateDTO;
  'scripts.changed': { rev: number };
  'settings.changed': { fields: string[] };
  'save.failed': { scriptId: string; name: string; message: string };
  'capture.hotkey': { capture_id: number; field: HotkeyField; hotkey: Hotkey | null; display: string };
  // mods: modifier state at the key-down (bridge/capture.go keyEvent); null/absent when the host does not report it.
  'capture.key': { capture_id: number; key: string; mods?: string[] | null };
  'capture.mouse': { capture_id: number; x: number; y: number };
  'capture.ended': { capture_id: number; reason: CaptureEndReason };
  // The main window's Settings recorder (bridge/capture.go): a single modifier arrives as { modifiers: [], key: 'rctrl' }.
  'settings.capture.hotkey': { capture_id: number; field: SettingsHotkeyField; hotkey: Hotkey | null; display: string };
  'settings.capture.ended': { capture_id: number; reason: CaptureEndReason };
  'editor.reset': { reason: string; session_id: string };
  'editor.closeRequested': { gen: number; sessionId?: string; reason: 'user' | 'app-closing' | 'owner-closing' };
  'editor.measureRequest': { token: number };
  'editor.measureAbort': { reason: 'fallback' | 'dpi' | 'timeout' };
  'editor.windowFailed': { gen: number; stage: string; message: string; recoveredPath?: string };
  'editor.windowClosed': { gen: number; saved: boolean; forced: boolean; recoveredPath?: string; reason?: string };
  'update.available': { tag: string };
  'update.progress': { pct: number };
  'update.failed': { message: string };
  'nav.goto': { route: string };
  'app.closing': { deadline_ms: number };
}

export type EventName = keyof Events;
