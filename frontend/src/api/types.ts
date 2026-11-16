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

export type ErrorCode =
  | 'BAD_REQUEST' | 'UNKNOWN_METHOD' | 'BUSY' | 'INTERNAL' | 'NOT_FOUND' | 'NAME_EMPTY'
  | 'NAME_TAKEN' | 'HOTKEY_CONFLICT' | 'HOTKEY_REGISTER_FAILED' | 'EDITOR_BUSY' | 'EDITOR_CLOSED'
  | 'SAVE_FAILED' | 'INVALID_SETTING' | 'UPDATE_FAILED' | 'NOT_LICENSED';

export interface ApiErrorBody {
  code: ErrorCode;
  message: string;
  field?: string;
  conflicts?: unknown[];
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
  'nav.goto': { route: string };
  'app.closing': Record<string, never>;
}

export type EventName = keyof Events;
