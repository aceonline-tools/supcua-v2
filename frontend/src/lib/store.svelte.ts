import { ApiError, type Client } from '../api/client';
import { EditorStore } from './editor.svelte';
import { OVERLAY_DEFAULTS } from './overlayStyle';
import type {
  BootState, GroupDTO, OverlayStyleDTO, ScriptSummary, SettingsDTO, SettingsPatch, StateDTO, ThemeSetting,
} from '../api/types';

export type Route = { name: 'scripts' } | { name: 'settings' } | { name: 'editor'; id: string | null };

export interface UpdateState {
  tag: string;
  applying: boolean;
  pct: number;
  failed: string;
}

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
  private sidebarSeq = 0;
  private sidebarPending = 0;
  private sidebarConfirmed = false;
  private sidebarChain: Promise<void> = Promise.resolve();
  private ovSeq = 0;
  private ovPending = 0;
  private ovConfirmed: OverlayStyleDTO | null = null;
  private ovTimer: ReturnType<typeof setTimeout> | null = null;
  private ovChain: Promise<void> = Promise.resolve();
  selectedId = $state<string | null>(null);
  route = $state<Route>({ name: 'scripts' });
  unsaved = $state<Record<string, string>>({});
  pendingDelete = $state<ScriptSummary | null>(null);
  restartPending = $state<string[]>([]);
  loaded = $state(false);
  error = $state('');
  systemDark = $state(true);
  update = $state<UpdateState | null>(null);
  pendingRoute: Route | null = null;
  private closingNotified = false;
  readonly editor: EditorStore;
  private disposers: (() => void)[] = [];

  constructor(private api: Client) {
    this.editor = new EditorStore(api, this);
  }

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
    if (typeof document === 'undefined') return;
    document.documentElement.dataset.theme = this.effectiveTheme;
    try { localStorage.setItem('supcua.theme', this.themeSetting); } catch { /* storage unavailable */ }
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
      this.api.on('state', (s) => { this.state = s; this.applyTheme(); this.editor.onActivated(); }),
      this.api.on('scripts.changed', () => void this.loadScripts()),
      this.api.on('settings.changed', () => void this.loadSettings()),
      this.api.on('update.available', (e) => { this.update = { tag: e.tag, applying: false, pct: 0, failed: '' }; }),
      this.api.on('update.progress', (e) => { if (this.update) this.update.pct = e.pct; }),
      this.api.on('update.failed', (e) => { if (this.update) { this.update.applying = false; this.update.failed = e.message; } }),
      this.api.on('save.failed', (e) => { this.unsaved[e.scriptId] = e.message; }),
      this.api.on('nav.goto', (e) => {
        if (e.route === 'settings') void this.navigate({ name: 'settings' });
        else if (e.route === 'scripts') void this.navigate({ name: 'scripts' });
      }),
      this.api.on('app.closing', (e) => void this.onAppClosing(e.deadline_ms)),
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
    const seq = this.sidebarSeq;
    const ovGen = this.ovSeq;
    const next = await this.api.call('settings.get');
    if (this.sidebarSeq !== seq || this.sidebarPending > 0) next.sidebar_collapsed = this.settings?.sidebar_collapsed ?? next.sidebar_collapsed;
    if (this.ovSeq !== ovGen || this.ovTimer !== null || this.ovPending > 0) next.overlay_style = this.settings?.overlay_style ?? next.overlay_style;
    this.settings = next;
  }

  // Every way out of the editor goes through the guarded autosave-and-close.
  async navigate(route: Route): Promise<boolean> {
    if (this.editor.sessionId) {
      this.pendingRoute = route;
      const r = await this.editor.close();
      if (r === 'failed' && this.editor.discardPrompt === null) this.pendingRoute = null;
      if (r !== 'closed') return false;
      return true;
    }
    this.route = route;
    return true;
  }

  go(route: Route): void {
    void this.navigate(route);
  }

  async onAppClosing(deadlineMs: number): Promise<void> {
    if (this.closingNotified) return;
    this.closingNotified = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const budget = new Promise<void>((resolve) => { timer = setTimeout(resolve, Math.max(0, (deadlineMs || 2000) - 300)); });
    try {
      await Promise.race([this.editor.flushForClose().catch(() => {}), budget]);
    } finally {
      clearTimeout(timer);
      await this.api.call('app.closingReady').catch(() => {});
    }
  }

  select(id: string | null): void {
    this.selectedId = id;
  }

  openEditor(id: string | null = this.selectedId): Promise<void> {
    return id ? this.editor.open(id) : Promise.resolve();
  }

  openNew(): Promise<void> {
    return this.editor.open(null);
  }

  async applyUpdate(): Promise<void> {
    if (!this.update) return;
    this.update = { ...this.update, applying: true, pct: 0, failed: '' };
    try {
      await this.api.call('update.apply');
    } catch {
      this.update = { ...this.update, applying: false, failed: 'Update failed.' };
    }
  }

  async skipUpdate(): Promise<void> {
    this.update = null;
    await this.api.call('update.skip').catch(() => {});
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

  get sidebarCollapsed(): boolean {
    return this.settings?.sidebar_collapsed ?? false;
  }

  // Toggles are applied optimistically and sent one at a time, so the backend ends on the last
  // successful one; a failure rolls back only when no newer toggle is waiting, to the last value
  // the backend confirmed.
  setSidebarCollapsed(collapsed: boolean): Promise<void> {
    if (!this.settings) return Promise.resolve();
    if (this.sidebarPending === 0) this.sidebarConfirmed = this.settings.sidebar_collapsed;
    const seq = ++this.sidebarSeq;
    this.sidebarPending++;
    this.settings.sidebar_collapsed = collapsed;
    const run = async () => {
      try {
        await this.api.call('settings.setSidebarCollapsed', { collapsed });
        this.sidebarConfirmed = collapsed;
      } catch (e) {
        if (seq === this.sidebarSeq && this.settings) this.settings.sidebar_collapsed = this.sidebarConfirmed;
        this.error = errorText(e);
      } finally {
        this.sidebarPending--;
      }
    };
    this.sidebarChain = this.sidebarChain.then(run);
    return this.sidebarChain;
  }

  get overlayStyle(): OverlayStyleDTO | null {
    return this.settings?.overlay_style ?? null;
  }

  // Overlay style edits apply optimistically, are coalesced for 120 ms (slider drags), and are sent
  // one at a time like the sidebar toggle; a failure rolls back to the last backend-confirmed style
  // unless a newer edit is waiting. The backend's clamped answer replaces the local value.
  setOverlayStyle(patch: Partial<OverlayStyleDTO>): Promise<void> {
    if (!this.settings) return Promise.resolve();
    this.beginOverlayEdit();
    this.settings.overlay_style = { ...this.settings.overlay_style, ...patch };
    this.ovTimer = setTimeout(() => { this.ovTimer = null; void this.sendOverlayStyle('overlay.setStyle'); }, 120);
    return this.ovChain;
  }

  resetOverlayStyle(): Promise<void> {
    if (!this.settings) return Promise.resolve();
    this.beginOverlayEdit();
    this.settings.overlay_style = { ...OVERLAY_DEFAULTS };
    return this.sendOverlayStyle('overlay.resetStyle');
  }

  // Every edit advances the generation at once: a response or failure only touches the visible
  // state when no newer edit exists, and a rollback goes to the last backend-confirmed style.
  private beginOverlayEdit(): void {
    if (this.ovPending === 0 && this.ovTimer === null && this.settings) this.ovConfirmed = { ...this.settings.overlay_style };
    if (this.ovTimer !== null) { clearTimeout(this.ovTimer); this.ovTimer = null; }
    this.ovSeq++;
  }

  private sendOverlayStyle(method: 'overlay.setStyle' | 'overlay.resetStyle'): Promise<void> {
    if (!this.settings) return this.ovChain;
    const style = { ...this.settings.overlay_style };
    const gen = this.ovSeq;
    this.ovPending++;
    const run = async () => {
      try {
        const eff = method === 'overlay.resetStyle' ? await this.api.call(method) : await this.api.call(method, style);
        this.ovConfirmed = eff;
        if (gen === this.ovSeq && this.settings) this.settings.overlay_style = eff;
      } catch (e) {
        if (gen === this.ovSeq && this.settings && this.ovConfirmed) this.settings.overlay_style = this.ovConfirmed;
        this.error = errorText(e);
      } finally {
        this.ovPending--;
      }
    };
    this.ovChain = this.ovChain.then(run);
    return this.ovChain;
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
