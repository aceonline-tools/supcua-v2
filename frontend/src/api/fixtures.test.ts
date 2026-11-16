import { describe, expect, it } from 'vitest';
import type { BootState, Events, GroupDTO, OverlayDTO, ScriptList, ScriptSummary, SettingsDTO, StateDTO } from './types';
import state from './fixtures/state.json';
import overlay from './fixtures/overlay.json';
import summary from './fixtures/script_summary.json';
import group from './fixtures/group.json';
import settings from './fixtures/settings.json';
import boot from './fixtures/boot_state.json';
import list from './fixtures/scripts_list.json';
import events from './fixtures/events.json';

// Assignment to the DTO types is the compile-time check (svelte-check); keys are compared at runtime.
const typed = {
  state: state as StateDTO, overlay: overlay as OverlayDTO, summary: summary as ScriptSummary,
  group: group as GroupDTO, settings: settings as SettingsDTO, boot: boot as BootState, list: list as ScriptList,
  events: events as unknown as { [K in keyof Events]: Events[K] },
};

describe('fixtures', () => {
  it('carry exactly the documented field names', () => {
    expect(Object.keys(typed.state).sort()).toEqual(['auto_active', 'editor_open', 'global_active', 'keyboard_locked', 'mouse_locked', 'overlay', 'running_id', 'running_name', 'theme']);
    expect(Object.keys(typed.summary).sort()).toEqual(['enabled', 'group', 'hotkey_label', 'hotkey_mode', 'id', 'name', 'run_mode', 'running']);
    expect(Object.keys(typed.group).sort()).toEqual(['all_enabled', 'collapsed', 'count', 'name']);
    expect(Object.keys(typed.settings)).toHaveLength(10);
    expect(Object.keys(typed.events).sort()).toEqual(['app.closing', 'nav.goto', 'save.failed', 'scripts.changed', 'settings.changed', 'state']);
  });
});
