import { ApiError, type Transport } from './client';
import type {
  RunMode, BootState, GroupDTO, ScriptSummary, SettingsDTO, StateDTO, ScriptList,
} from './types';

export interface MockSeed {
  scripts: ScriptSummary[];
  settings: SettingsDTO;
  state: StateDTO;
  boot: BootState;
  collapsed: string[];
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
      auto_active_enabled: true, auto_active_process_name: 'game.exe', collapsed_groups: [],
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

const RESTART_ONLY = new Set(['key_press_duration_ms', 'scripts_dir']);

export class MockBackend implements Transport {
  readonly calls: { method: string; params: unknown }[] = [];
  failSaveFor = new Set<string>();
  private seed: MockSeed;
  private rev = 1;
  private nextId = 100;
  private emit: (name: string, payload: unknown) => void = () => {};

  constructor(seed: MockSeed = defaultSeed()) {
    this.seed = structuredClone(seed);
  }

  subscribe(handler: (name: string, payload: unknown) => void): void {
    this.emit = handler;
  }

  push(name: string, payload: unknown): void {
    this.emit(name, payload);
  }

  async call(method: string, params: unknown): Promise<unknown> {
    this.calls.push({ method, params: structuredClone(params) });
    await Promise.resolve();
    return structuredClone(this.handle(method, (params ?? {}) as Record<string, any>));
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

  private handle(method: string, p: Record<string, any>): unknown {
    const st = this.seed;
    switch (method) {
      case 'app.getBootState': return st.boot;
      case 'app.ready': return {};
      case 'state.get': return st.state;
      case 'app.setGlobalActive': st.state.global_active = !!p.active; return st.state;
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
        return { rev: ++this.rev };
      }
      case 'scripts.retrySave': { this.find(p.id); this.failSaveFor.delete(p.id); return {}; }
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
