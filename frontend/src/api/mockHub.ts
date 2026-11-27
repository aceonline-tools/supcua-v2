import { ApiError, type Transport } from './client';
import { MockBackend, defaultSeed, type MockSeed } from './mock';

export type HubRole = 'main' | 'editor';
type Target = 'both' | 'main' | 'editor';

// Mirrors bridge/roles.go Roles(): main = Both + MainOnly, editor = Both + EditorOnly, anything else denied for the editor.
const BOTH = ['app.getBootState', 'app.ready', 'state.get', 'actions.catalog', 'clipboard.write', 'settings.get', 'app.closingReady'];
const MAIN_ONLY = [
  'app.setGlobalActive', 'app.exit', 'window.hide', 'window.show', 'window.reportBounds',
  'scripts.list', 'scripts.get', 'scripts.setEnabled', 'scripts.setGroupEnabled', 'scripts.setGroupCollapsed',
  'scripts.clone', 'scripts.delete', 'scripts.retrySave', 'settings.update', 'settings.setSidebarCollapsed',
  'overlay.set', 'overlay.setStyle', 'overlay.resetStyle', 'autoactive.set', 'update.apply', 'update.skip',
  'editor.openWindow',
];
// Selftest-only gen-0 route (bridge/roles.go SelftestOnly): denied to both windows in production.
const SELFTEST_ONLY = ['editor.close'];
const EDITOR_ONLY = [
  'editor.windowReady', 'editor.measured', 'editor.measureAck', 'editor.checkpoint', 'editor.guardActive', 'editor.closeDecision',
  'editor.requestClose', 'editor.open', 'editor.save', 'editor.describeActions', 'editor.toJSON', 'editor.fromJSON',
  'capture.hotkey.start', 'capture.key.start', 'capture.mouse.start', 'capture.mouse.stop', 'capture.stop',
];
const ALLOWED: Record<HubRole, Set<string>> = {
  main: new Set([...BOTH, ...MAIN_ONLY]),
  editor: new Set([...BOTH, ...EDITOR_ONLY]),
};

// Contract §2.4 event routing table.
const EVENT_TARGET: Record<string, Target> = {
  state: 'both', 'scripts.changed': 'both', 'settings.changed': 'both', 'save.failed': 'both', 'app.closing': 'both',
  'editor.windowFailed': 'main', 'editor.windowClosed': 'main', 'update.available': 'main', 'update.progress': 'main', 'update.failed': 'main', 'nav.goto': 'main',
};
const targetOf = (name: string): Target => EVENT_TARGET[name] ?? 'editor';

export interface HubHost {
  /** The host must create the editor window for `gen` (hidden until measured); `id` null means a new script. */
  createEditorWindow(gen: number, id: string | null): void;
  /** The editor session ended; destroy the window. */
  destroyEditorWindow(gen: number): void;
}

type Handler = (name: string, payload: unknown) => void;
interface Channel { role: HubRole; gen: number; handler: Handler }

/**
 * One MockBackend shared by the main and editor windows (browser simulator only). Each window gets its own
 * Transport: role and generation are the channel's, never the page's params, like the host's Caller.
 */
export class MockHub {
  readonly backend: MockBackend;
  private channels = new Set<Channel>();
  private liveGen = 0;

  constructor(private host: HubHost, seed: MockSeed = defaultSeed()) {
    this.backend = new MockBackend(seed);
    this.backend.subscribe((name, payload) => this.route(name, payload));
  }

  get editorGen(): number {
    return this.liveGen;
  }

  transport(role: HubRole, gen = 0): Transport {
    const ch: Channel = { role, gen, handler: () => {} };
    this.channels.add(ch);
    return {
      call: (method, params) => this.call(ch, method, params),
      subscribe: (h) => { ch.handler = h; },
    };
  }

  /** Detaches every channel of a destroyed editor generation (its late events are dropped). */
  private detach(gen: number): void {
    for (const c of this.channels) if (c.role === 'editor' && c.gen === gen) this.channels.delete(c);
  }

  /** WM_CLOSE on the editor window: the page runs its guard and ends with closeDecision. */
  hostCloseRequest(): void {
    if (!this.liveGen) return;
    void this.backend.call('editor.requestClose', {});
  }

  /** Create/embed/measure failure on the host side: roll back and tell main. */
  failWindow(stage: string, message: string): void {
    const gen = this.liveGen;
    if (!gen) return;
    this.endWindow(gen);
    this.backend.abortOpen();
    this.route('editor.windowFailed', { gen, stage, message });
  }

  private endWindow(gen: number): void {
    if (this.liveGen !== gen) return;
    this.liveGen = 0;
    this.detach(gen);
    this.host.destroyEditorWindow(gen);
  }

  private route(name: string, payload: unknown): void {
    const target = targetOf(name);
    for (const c of [...this.channels]) {
      if (target !== 'both' && target !== c.role) continue;
      if (c.role === 'editor' && (c.gen !== this.liveGen || !this.liveGen)) continue;
      try { c.handler(name, structuredClone(payload)); } catch (e) { console.error('hub event handler failed', name, e); }
    }
  }

  private async call(ch: Channel, method: string, params: unknown): Promise<unknown> {
    if (SELFTEST_ONLY.includes(method)) throw new ApiError({ code: 'FORBIDDEN_ROLE', message: 'Not allowed from this window' });
    if (!ALLOWED[ch.role].has(method)) {
      if (ch.role === 'editor') throw new ApiError({ code: 'FORBIDDEN_ROLE', message: 'Not allowed from this window' });
      if (EDITOR_ONLY.includes(method)) throw new ApiError({ code: 'FORBIDDEN_ROLE', message: 'Not allowed from this window' });
    }
    if (ch.role === 'editor' && (ch.gen === 0 || ch.gen !== this.liveGen)) throw new ApiError({ code: 'STALE_GEN', message: 'Stale editor window' });
    const p = (params ?? {}) as Record<string, unknown>;
    if (method === 'editor.open' && !(Object.hasOwn(p, 'id') && (p.id === null || typeof p.id === 'string'))) {
      throw new ApiError({ code: 'BAD_REQUEST', message: 'editor.open requires id (string or null)' });
    }
    if (method === 'editor.openWindow' && p.id !== undefined && typeof p.id !== 'string') {
      throw new ApiError({ code: 'BAD_REQUEST', message: 'id must be a string' });
    }
    const result = await this.backend.call(method, params);
    if (method === 'editor.openWindow' && (result as { status: string }).status === 'opening') {
      const gen = (result as { gen: number }).gen;
      this.liveGen = gen;
      this.host.createEditorWindow(gen, typeof p.id === 'string' ? p.id : null);
    } else if (method === 'editor.closeDecision' && ch.role === 'editor' && p.decision !== 'cancel') {
      const gen = this.liveGen;
      this.endWindow(gen);
      this.route('editor.windowClosed', { gen, saved: p.decision === 'saved', forced: false });
    }
    return result;
  }
}
