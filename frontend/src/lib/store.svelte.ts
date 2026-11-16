import { ApiError, type Client } from '../api/client';
import type {
  BootState, GroupDTO, ScriptSummary, SettingsDTO, SettingsPatch, StateDTO, ThemeSetting,
} from '../api/types';

export type Route = { name: 'scripts' } | { name: 'settings' } | { name: 'editor'; id: string | null };

export type Row =
  | { kind: 'script'; script: ScriptSummary; grouped: boolean }
  | { kind: 'group'; group: GroupDTO; checked: boolean; indeterminate: boolean; collapsed: boolean };

export interface SettingResult {
  ok: boolean;
  field?: string;
}

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : 'Something went wrong';
}

export class AppStore {
  boot = $state<BootState | null>(null);
  state = $state<StateDTO | null>(null);
  settings = $state<SettingsDTO | null>(null);
  scripts = $state<ScriptSummary[]>([]);
  groups = $state<GroupDTO[]>([]);
  collapsed = $state<string[]>([]);
  selectedId = $state<string | null>(null);
  route = $state<Route>({ name: 'scripts' });
  unsaved = $state<Record<string, string>>({});
  pendingDelete = $state<ScriptSummary | null>(null);
  restartPending = $state<string[]>([]);
  loaded = $state(false);
  error = $state('');
  systemDark = $state(true);
  private disposers: (() => void)[] = [];

  constructor(private api: Client) {}

  get themeSetting(): ThemeSetting {
    return this.state?.theme ?? 'dark';
  }

  get effectiveTheme(): 'dark' | 'light' {
    const t = this.themeSetting;
    return t === 'system' ? (this.systemDark ? 'dark' : 'light') : t;
  }

  get selected(): ScriptSummary | undefined {
    return this.scripts.find((s) => s.id === this.selectedId);
  }

  get enabledCount(): number {
    return this.scripts.filter((s) => s.enabled).length;
  }

  get rows(): Row[] {
    const rows: Row[] = [];
    for (const s of this.scripts) if (s.group === '') rows.push({ kind: 'script', script: s, grouped: false });
    for (const g of this.groups) {
      const members = this.scripts.filter((s) => s.group === g.name);
      const on = members.filter((m) => m.enabled).length;
      const collapsed = this.collapsed.includes(g.name);
      rows.push({
        kind: 'group', group: { ...g, count: members.length }, collapsed,
        checked: members.length > 0 && on === members.length,
        indeterminate: on > 0 && on < members.length,
      });
      if (!collapsed) for (const s of members) rows.push({ kind: 'script', script: s, grouped: true });
    }
    return rows;
  }

  applyTheme(): void {
    if (typeof document !== 'undefined') document.documentElement.dataset.theme = this.effectiveTheme;
  }

  async init(): Promise<void> {
    if (typeof window !== 'undefined' && window.matchMedia) {
      const mq = window.matchMedia('(prefers-color-scheme: dark)');
      this.systemDark = mq.matches;
      const onChange = (e: MediaQueryListEvent) => {
        this.systemDark = e.matches;
        this.applyTheme();
      };
      mq.addEventListener('change', onChange);
      this.disposers.push(() => mq.removeEventListener('change', onChange));
    }
    this.disposers.push(
      this.api.on('state', (s) => { this.state = s; this.applyTheme(); }),
      this.api.on('scripts.changed', () => void this.loadScripts()),
      this.api.on('settings.changed', () => void this.loadSettings()),
      this.api.on('save.failed', (e) => { this.unsaved[e.scriptId] = e.message; }),
      this.api.on('nav.goto', (e) => {
        if (e.route === 'settings') this.route = { name: 'settings' };
        else if (e.route === 'scripts') this.route = { name: 'scripts' };
      }),
    );
    try {
      const [boot, state] = await Promise.all([this.api.call('app.getBootState'), this.api.call('state.get')]);
      this.boot = boot;
      this.state = state;
      this.applyTheme();
      await Promise.all([this.loadScripts(), this.loadSettings()]);
      await this.api.call('app.ready');
    } catch (e) {
      this.error = errorText(e);
    } finally {
      this.loaded = true;
    }
  }

  dispose(): void {
    this.disposers.splice(0).forEach((d) => d());
  }

  async loadScripts(): Promise<void> {
    const list = await this.api.call('scripts.list');
    this.scripts = list.scripts;
    this.groups = list.groups;
    this.collapsed = list.groups.filter((g) => g.collapsed).map((g) => g.name);
    if (this.selectedId && !list.scripts.some((s) => s.id === this.selectedId)) this.selectedId = null;
  }

  async loadSettings(): Promise<void> {
    this.settings = await this.api.call('settings.get');
  }

  go(route: Route): void {
    this.route = route;
  }

  select(id: string | null): void {
    this.selectedId = id;
  }

  openEditor(id: string | null = this.selectedId): void {
    if (id) this.route = { name: 'editor', id };
  }

  openNew(): void {
    this.route = { name: 'editor', id: null };
  }

  async setGlobalActive(active: boolean): Promise<void> {
    try {
      this.state = await this.api.call('app.setGlobalActive', { active });
    } catch (e) {
      this.error = errorText(e);
    }
  }

  async setEnabled(id: string, enabled: boolean): Promise<void> {
    const sc = this.scripts.find((s) => s.id === id);
    if (!sc) return;
    const prev = sc.enabled;
    sc.enabled = enabled;
    try {
      await this.api.call('scripts.setEnabled', { id, enabled });
    } catch (e) {
      if (e instanceof ApiError && e.code === 'SAVE_FAILED') this.unsaved[id] = e.message;
      else { sc.enabled = prev; this.error = errorText(e); }
    }
  }

  async setGroupEnabled(group: string, enabled: boolean): Promise<void> {
    const members = this.scripts.filter((s) => s.group === group);
    const prev = members.map((m) => m.enabled);
    members.forEach((m) => (m.enabled = enabled));
    try {
      await this.api.call('scripts.setGroupEnabled', { group, enabled });
    } catch (e) {
      if (e instanceof ApiError && e.code === 'SAVE_FAILED') members.forEach((m) => (this.unsaved[m.id] = e.message));
      else { members.forEach((m, i) => (m.enabled = prev[i])); this.error = errorText(e); }
    }
  }

  async toggleCollapsed(group: string): Promise<void> {
    const was = this.collapsed.includes(group);
    this.collapsed = was ? this.collapsed.filter((g) => g !== group) : [...this.collapsed, group];
    try {
      await this.api.call('scripts.setGroupCollapsed', { group, collapsed: !was });
    } catch (e) {
      this.collapsed = was ? [...this.collapsed, group] : this.collapsed.filter((g) => g !== group);
      this.error = errorText(e);
    }
  }

  async cloneSelected(): Promise<void> {
    const id = this.selectedId;
    if (!id) return;
    try {
      const copy = await this.api.call('scripts.clone', { id });
      await this.loadScripts();
      this.selectedId = copy.id;
    } catch (e) {
      this.error = errorText(e);
    }
  }

  requestDelete(): void {
    this.pendingDelete = this.selected ?? null;
  }

  cancelDelete(): void {
    this.pendingDelete = null;
  }

  async confirmDelete(): Promise<void> {
    const target = this.pendingDelete;
    this.pendingDelete = null;
    if (!target) return;
    try {
      await this.api.call('scripts.delete', { id: target.id });
      delete this.unsaved[target.id];
      await this.loadScripts();
    } catch (e) {
      this.error = errorText(e);
    }
  }

  async retrySave(id: string): Promise<void> {
    try {
      await this.api.call('scripts.retrySave', { id });
      delete this.unsaved[id];
    } catch (e) {
      this.error = errorText(e);
    }
  }

  async setTheme(theme: ThemeSetting): Promise<void> {
    if (!this.state || this.state.theme === theme) return;
    const prev = this.state.theme;
    this.state.theme = theme;
    this.applyTheme();
    try {
      const res = await this.api.call('settings.update', { patch: { theme } });
      this.state = res.state;
      if (this.settings) this.settings.theme = theme;
    } catch (e) {
      this.state.theme = prev;
      this.applyTheme();
      this.error = errorText(e);
    }
  }

  async updateSettings(patch: SettingsPatch): Promise<SettingResult> {
    try {
      const res = await this.api.call('settings.update', { patch });
      this.state = res.state;
      if (this.settings) Object.assign(this.settings, patch);
      this.restartPending = [...new Set([...this.restartPending, ...res.restartRequired])];
      return { ok: true };
    } catch (e) {
      await this.loadSettings().catch(() => {});
      if (e instanceof ApiError && e.code === 'INVALID_SETTING') return { ok: false, field: e.body.field };
      this.error = errorText(e);
      return { ok: false };
    }
  }

  async setAutoActive(p: { enabled?: boolean; processName?: string }): Promise<SettingResult> {
    try {
      const res = await this.api.call('autoactive.set', p);
      if (this.state) this.state.auto_active = { enabled: res.enabled, process_name: res.processName };
      return { ok: true };
    } catch (e) {
      this.error = errorText(e);
      return { ok: false };
    }
  }

  async setOverlay(p: { enabled?: boolean; visible?: boolean; draggable?: boolean }): Promise<void> {
    try {
      const overlay = await this.api.call('overlay.set', p);
      if (this.state) this.state.overlay = overlay;
      if (this.settings) this.settings.overlay_enabled = overlay.enabled;
    } catch (e) {
      this.error = errorText(e);
    }
  }
}
