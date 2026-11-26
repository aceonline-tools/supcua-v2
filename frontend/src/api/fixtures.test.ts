import { validEventPayload } from './validate';
import { describe, expect, it } from 'vitest';
import type { ActionSpec, BootState, EditorSession, HotkeyConflict, Script, Events, GroupDTO, OverlayDTO, ScriptList, ScriptSummary, SettingsDTO, StateDTO } from './types';
import state from './fixtures/state.json';
import overlay from './fixtures/overlay.json';
import summary from './fixtures/script_summary.json';
import group from './fixtures/group.json';
import settings from './fixtures/settings.json';
import boot from './fixtures/boot_state.json';
import list from './fixtures/scripts_list.json';
import script from './fixtures/script.json';
import spec from './fixtures/action_spec.json';
import session from './fixtures/editor_session.json';
import conflict from './fixtures/hotkey_conflict.json';
import events from './fixtures/events.json';

// Assignment to the DTO types is the compile-time check (svelte-check); keys are compared at runtime.
const typed = {
  state: state as StateDTO, overlay: overlay as OverlayDTO, summary: summary as ScriptSummary,
  group: group as GroupDTO, settings: settings as SettingsDTO, boot: boot as BootState, list: list as ScriptList,
  script: script as Script, spec: spec as ActionSpec, session: session as EditorSession, conflict: conflict as HotkeyConflict,
  events: events as unknown as { [K in keyof Events]: Events[K] },
};

describe('fixtures', () => {
  it('carry exactly the documented field names', () => {
    expect(Object.keys(typed.state).sort()).toEqual(['auto_active', 'editor_open', 'global_active', 'keyboard_locked', 'mouse_locked', 'overlay', 'running_id', 'running_name', 'theme']);
    expect(Object.keys(typed.summary).sort()).toEqual(['enabled', 'group', 'hotkey_label', 'hotkey_mode', 'id', 'name', 'run_mode', 'running']);
    expect(Object.keys(typed.group).sort()).toEqual(['all_enabled', 'collapsed', 'count', 'name']);
    expect(Object.keys(typed.settings)).toHaveLength(11);
    expect(Object.keys(typed.events).sort()).toEqual(['app.closing', 'capture.ended', 'capture.hotkey', 'capture.key', 'capture.mouse', 'editor.reset', 'nav.goto', 'save.failed', 'scripts.changed', 'settings.changed', 'state', 'update.available', 'update.failed', 'update.progress']);
    expect(Object.keys(typed.script).sort()).toEqual(['actions', 'enabled', 'group', 'hotkey_mode', 'id', 'name', 'run_mode', 'start_hotkey', 'stop_hotkey', 'toggle_hotkey']);
    expect(Object.keys(typed.conflict).sort()).toEqual(['field', 'hotkey', 'name', 'scriptId']);
    expect(Object.keys(typed.spec).sort()).toEqual(['group', 'params', 'type']);
    expect(Object.keys(typed.session).sort()).toEqual(['catalog', 'script', 'sessionId']);
  });

  it('every event fixture passes the payload validator; broken variants fail', () => {
    for (const [name, payload] of Object.entries(events)) expect(validEventPayload(name, payload), name).toBe(true);
    expect(validEventPayload('capture.key', { key: 'a' })).toBe(false);
    expect(validEventPayload('capture.ended', { capture_id: 1, reason: 'sessionReset' })).toBe(false);
    expect(validEventPayload('state', { ...events.state, theme: 'blue' })).toBe(false);
    expect(validEventPayload('app.closing', {})).toBe(false);
    expect(validEventPayload('unknown.event', {})).toBe(false);
  });
});
