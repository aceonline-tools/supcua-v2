import type { EventName } from './types';

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown) => typeof v === 'string';
const bool = (v: unknown) => typeof v === 'boolean';
const num = (v: unknown) => typeof v === 'number' && Number.isFinite(v);
const int = (v: unknown) => Number.isInteger(v);
const oneOf = (v: unknown, set: readonly string[]) => typeof v === 'string' && set.includes(v);
const hotkey = (v: unknown) => isObj(v) && Array.isArray(v.modifiers) && v.modifiers.every(str) && str(v.key);

const overlay = (v: unknown) => isObj(v) && bool(v.enabled) && bool(v.visible) && bool(v.draggable) && num(v.x) && num(v.y);

const validators: Record<EventName, (v: Obj) => boolean> = {
  state: (v) =>
    bool(v.global_active) && str(v.running_id) && str(v.running_name) && bool(v.editor_open) && overlay(v.overlay) &&
    oneOf(v.theme, ['light', 'dark', 'system']) && bool(v.mouse_locked) && bool(v.keyboard_locked) &&
    isObj(v.auto_active) && bool(v.auto_active.enabled) && str(v.auto_active.process_name),
  'scripts.changed': (v) => int(v.rev),
  'settings.changed': (v) => Array.isArray(v.fields) && v.fields.every(str),
  'save.failed': (v) => str(v.scriptId) && str(v.name) && str(v.message),
  'nav.goto': (v) => str(v.route),
  'app.closing': (v) => num(v.deadline_ms),
  'editor.reset': (v) => str(v.reason) && str(v.session_id),
  'editor.closeRequested': (v) => int(v.gen) && oneOf(v.reason, ['user', 'app-closing', 'owner-closing']) && (v.sessionId === undefined || str(v.sessionId)),
  'editor.measureRequest': (v) => int(v.token),
  'editor.measureAbort': (v) => oneOf(v.reason, ['fallback', 'dpi', 'timeout']),
  'editor.windowFailed': (v) => int(v.gen) && str(v.stage) && str(v.message) && (v.recoveredPath === undefined || str(v.recoveredPath)),
  'editor.windowClosed': (v) => int(v.gen) && bool(v.saved) && bool(v.forced) && (v.recoveredPath === undefined || str(v.recoveredPath)),
  'capture.hotkey': (v) => int(v.capture_id) && oneOf(v.field, ['toggle', 'start', 'stop']) && (v.hotkey === null || hotkey(v.hotkey)) && str(v.display),
  'capture.key': (v) => int(v.capture_id) && str(v.key),
  'capture.mouse': (v) => int(v.capture_id) && num(v.x) && num(v.y),
  'capture.ended': (v) => int(v.capture_id) && oneOf(v.reason, ['stopped', 'blur', 'timeout', 'editorClosed']),
  'update.available': (v) => str(v.tag),
  'update.progress': (v) => num(v.pct),
  'update.failed': (v) => str(v.message),
};

export function isKnownEvent(name: string): name is EventName {
  return Object.prototype.hasOwnProperty.call(validators, name);
}

export function validEventPayload(name: string, payload: unknown): boolean {
  return isKnownEvent(name) && isObj(payload) && validators[name](payload);
}
