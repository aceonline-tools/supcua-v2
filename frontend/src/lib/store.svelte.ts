import { ApiError, type Client } from '../api/client';
import { EditorStore } from './editor.svelte';
import { OVERLAY_DEFAULTS } from './overlayStyle';
import type { PageRole } from './role';
import type {
  BootState, GroupDTO, UpdateCheckError, OverlayStyleDTO, ScriptSummary, SettingsDTO, SettingsPatch, StateDTO, ThemeSetting,
} from '../api/types';

/** bridge.UpdateStaleMessage: the offer changed after the page showed it. */
const STALE_UPDATE_MESSAGE = 'A newer update was found. Check again.';

export type Route = { name: 'scripts' } | { name: 'settings' };

export interface UpdateState {
  tag: string;
  applying: boolean;
  pct: number;
  failed: string;
}

export type UpdateCheckState =
  | { phase: 'idle' }
  | { phase: 'checking' }
  | { phase: 'up_to_date'; current: string }
  | { phase: 'available'; tag: string }
  | { phase: 'applying'; tag: string; pct: number }
  | { phase: 'error'; kind: UpdateCheckError };

export type Row =
  | { kind: 'script'; script: ScriptSummary; grouped: boolean }
  | { kind: 'group'; group: GroupDTO; checked: boolean; indeterminate: boolean; collapsed: boolean };

export interface SettingResult {
  ok: boolean;
  field?: string;
  code?: string;
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
  // Overlay style: ovServer = last value known from the backend (setter confirmation or fetch);
  // ovUser holds only the fields the user edited that are not yet confirmed (ovDirty: field -> edit
  // number). The visible style is ovServer overlaid with ovUser, so external changes (a drag) reach
  // every field the user has not touched. ovConfirms counts setter confirmations; a settings fetch
  // that started before one is stale for the overlay and is dropped and repeated.
  private ovServer: OverlayStyleDTO | null = null;
  private ovUser: Partial<OverlayStyleDTO> = {};
  private ovDirty = new Map<keyof OverlayStyleDTO, number>();
  private ovEditSeq = 0;
  private ovConfirms = 0;
  private ovPending = 0;
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
  updateCheck = $state<UpdateCheckState>({ phase: 'idle' });
  private closingNotified = false;
  // Only the editor window has an EditorStore; the main window opens the editor with editor.openWindow.
  readonly editor!: EditorStore;
  private disposers: (() => void)[] = [];

  constructor(readonly api: Client, readonly role: PageRole = 'main') {
    if (role === 'editor') this.editor = new EditorStore(api, this);
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
      this.api.on('state', (s) => { this.state = s; this.applyTheme(); this.editor?.onActivated(); }),
      this.api.on('settings.changed', () => void this.loadSettings()),
    );
    if (this.role === 'main') this.disposers.push(
      this.api.on('scripts.changed', () => void this.loadScripts()),
      this.api.on('update.available', (e) => { this.update = { tag: e.tag, applying: false, pct: 0, failed: '' }; }),
      this.api.on('update.progress', (e) => {
        if (this.update) this.update.pct = e.pct;
        if (this.updateCheck.phase === 'applying') this.updateCheck = { ...this.updateCheck, pct: e.pct };
      }),
      this.api.on('update.failed', (e) => {
        if (this.update) { this.update.applying = false; this.update.failed = e.message; }
        if (this.updateCheck.phase === 'applying') this.updateCheck = { phase: 'error', kind: e.message === STALE_UPDATE_MESSAGE ? 'stale' : 'other' };
      }),
      this.api.on('save.failed', (e) => { this.unsaved[e.scriptId] = e.message; }),
      this.api.on('nav.goto', (e) => {
        if (e.route === 'settings') this.route = { name: 'settings' };
        else if (e.route === 'scripts') this.route = { name: 'scripts' };
      }),
      this.api.on('editor.windowFailed', (e) => {
        this.error = e.recoveredPath ? `The editor stopped; your draft was saved to ${e.recoveredPath}` : `The editor stopped: ${e.message}`;
      }),
      this.api.on('editor.windowClosed', (e) => {
        if (e.recoveredPath) this.error = `Your draft was saved to ${e.recoveredPath}`;
      }),
      this.api.on('app.closing', () => void this.onAppClosing()),
    );
    try {
      this.boot = await this.api.call('app.getBootState');
      if (this.boot.mode !== 'main') {
        await this.api.call('app.ready');
        return;
      }
      this.state = await this.api.call('state.get');
      this.applyTheme();
      await Promise.all([this.role === 'main' ? this.loadScripts() : Promise.resolve(), this.loadSettings()]);
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
    const confirms = this.ovConfirms;
    const next = await this.api.call('settings.get');
    if (this.sidebarSeq !== seq || this.sidebarPending > 0) next.sidebar_collapsed = this.settings?.sidebar_collapsed ?? next.sidebar_collapsed;
    const stale = this.ovServer !== null && confirms !== this.ovConfirms;
    if (!stale) this.ovServer = { ...next.overlay_style };
    this.settings = next;
    this.composeOverlay();
    if (stale) void this.loadSettings();
  }

  go(route: Route): void {
    this.route = route;
  }

  async onAppClosing(): Promise<void> {
    if (this.closingNotified) return;
    this.closingNotified = true;
    await this.api.call('app.closingReady').catch(() => {});
  }

  select(id: string | null): void {
    this.selectedId = id;
  }

  async openEditor(id: string | null = this.selectedId): Promise<void> {
    if (id) await this.openWindow({ id });
  }

  openNew(): Promise<void> {
    return this.openWindow({});
  }

  private async openWindow(params: { id?: string }): Promise<void> {
    try {
      const r = await this.api.call('editor.openWindow', params);
      if (r.status === 'focused') this.error = 'Editor already open';
    } catch (e) {
      this.error = errorText(e);
    }
  }

  async applyUpdate(): Promise<void> {
    if (!this.update) return;
    this.update = { ...this.update, applying: true, pct: 0, failed: '' };
    try {
      await this.api.call('update.apply', { version: this.update.tag });
    } catch {
      this.update = { ...this.update, applying: false, failed: 'Update failed.' };
    }
  }

  /** On-demand release check for Settings; the backend is single-flight, this only avoids a pointless call. */
  async checkForUpdates(): Promise<void> {
    if (this.updateCheck.phase === 'checking' || this.updateCheck.phase === 'applying') return;
    this.updateCheck = { phase: 'checking' };
    try {
      const r = await this.api.call('update.check');
      if (r.status === 'up_to_date') this.updateCheck = { phase: 'up_to_date', current: r.current };
      else if (r.status === 'available' && r.tag) this.updateCheck = { phase: 'available', tag: r.tag };
      else this.updateCheck = { phase: 'error', kind: r.error ?? 'other' };
    } catch {
      this.updateCheck = { phase: 'error', kind: 'other' };
    }
  }

  /** Installs the update found by checkForUpdates through the same update.apply path as the startup prompt. */
  async installCheckedUpdate(): Promise<void> {
    const c = this.updateCheck;
    if (c.phase !== 'available') return;
    this.updateCheck = { phase: 'applying', tag: c.tag, pct: 0 };
    try {
      await this.api.call('update.apply', { version: c.tag });
    } catch {
      this.updateCheck = { phase: 'error', kind: 'other' };
    }
  }

  async skipUpdate(): Promise<void> {
    const version = this.update?.tag;
    this.update = null;
    if (version) await this.api.call('update.skip', { version }).catch(() => {});
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
    if (!this.settings || !this.ovServer) return Promise.resolve();
    this.markDirty(patch);
    if (this.ovTimer !== null) clearTimeout(this.ovTimer);
    this.ovTimer = setTimeout(() => { this.ovTimer = null; void this.sendOverlayStyle('overlay.setStyle'); }, 120);
    return this.ovChain;
  }

  // Reset is an edit of every field (to the defaults), so it overrides earlier pending edits and
  // loses to later ones.
  resetOverlayStyle(): Promise<void> {
    if (!this.settings || !this.ovServer) return Promise.resolve();
    if (this.ovTimer !== null) { clearTimeout(this.ovTimer); this.ovTimer = null; }
    this.markDirty({ ...OVERLAY_DEFAULTS });
    return this.sendOverlayStyle('overlay.resetStyle');
  }

  private markDirty(patch: Partial<OverlayStyleDTO>): void {
    for (const k of Object.keys(patch) as (keyof OverlayStyleDTO)[]) {
      (this.ovUser as Record<string, unknown>)[k] = patch[k];
      this.ovDirty.set(k, ++this.ovEditSeq);
    }
    this.composeOverlay();
  }

  private composeOverlay(): void {
    if (this.settings && this.ovServer) this.settings.overlay_style = { ...this.ovServer, ...this.ovUser };
  }

  // Edits made after a send keep their fields dirty; the rest is settled by the response.
  private settleOverlay(sent: Map<keyof OverlayStyleDTO, number>): void {
    for (const [k, n] of sent) {
      if (this.ovDirty.get(k) === n) { this.ovDirty.delete(k); delete this.ovUser[k]; }
    }
  }

  private sendOverlayStyle(method: 'overlay.setStyle' | 'overlay.resetStyle'): Promise<void> {
    if (!this.settings || !this.ovServer) return this.ovChain;
    const sentKeys = new Map(this.ovDirty);
    this.ovPending++;
    const run = async () => {
      // Built when the request actually goes out: user-edited fields plus the newest external
      // values (a drag that happened meanwhile).
      const style = { ...this.ovServer!, ...this.ovUser } as OverlayStyleDTO;
      try {
        const eff = method === 'overlay.resetStyle' ? await this.api.call(method) : await this.api.call(method, style);
        this.ovConfirms++;
        const server = this.ovServer!;
        for (const k of Object.keys(eff) as (keyof OverlayStyleDTO)[]) {
          if (sentKeys.has(k) || style[k] !== eff[k]) (server as unknown as Record<string, unknown>)[k] = eff[k];
        }
        this.settleOverlay(sentKeys);
      } catch (e) {
        this.settleOverlay(sentKeys);
        this.error = errorText(e);
      } finally {
        this.ovPending--;
        this.composeOverlay();
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
      if (e instanceof ApiError && e.code === 'INVALID_SETTING') return { ok: false, field: e.body.field, code: e.code };
      if (e instanceof ApiError && (e.code === 'HOTKEY_REGISTER_FAILED' || e.code === 'HOTKEY_RESTORE_FAILED')) return { ok: false, code: e.code }; // shown inline by the recorder
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
