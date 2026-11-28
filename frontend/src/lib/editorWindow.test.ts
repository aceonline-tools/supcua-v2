import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient, type Client } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from './store.svelte';
import { applyScrollbarMetrics, MeasureController, scrollbarWidth } from './measure';
import { CheckpointSender, draftBytes, MAX_DRAFT_BYTES } from './checkpoint';
import { pageGen, pageRole } from './role';
import { decisions, editorStore } from './test-editor';
import { GUARD_PING_MS } from './editor.svelte';
import App from '../App.svelte';
import EditorApp from '../components/EditorApp.svelte';
import typesSrc from '../api/types.ts?raw';
import storeSrc from './store.svelte.ts?raw';
import appSrc from '../App.svelte?raw';

const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); flushSync(); };
const calls = (m: MockBackend, name: string) => m.calls.filter((c) => c.method === name);

afterEach(() => { vi.useRealTimers(); history.replaceState(null, '', '/'); delete document.documentElement.dataset.measure; document.documentElement.style.removeProperty('--measure-w'); });

describe('routing', () => {
  it('?window=editor selects the editor role and gen; anything else is main', () => {
    expect(pageRole('?window=editor&gen=3')).toBe('editor');
    expect(pageGen('?window=editor&gen=3')).toBe(3);
    expect(pageRole('')).toBe('main');
    expect(pageRole('?window=other')).toBe('main');
    expect(pageGen('?window=editor&gen=-1')).toBe(0);
    expect(pageGen('?window=editor&gen=abc')).toBe(0);
  });

  it('the editor app renders no sidebar, Scripts or Settings; the main app renders no editor', async () => {
    const mock = new MockBackend();
    const store = await editorStore(mock, 's3');
    const host = document.createElement('div');
    document.body.replaceChildren(host);
    const ed = mount(EditorApp, { target: host, props: { store } });
    await settle();
    expect(host.querySelector('.sidebar')).toBeNull();
    expect(host.querySelector('#editor-root')).not.toBeNull();
    expect(host.textContent).not.toContain('Script library');
    await unmount(ed);
    const mock2 = new MockBackend();
    const main = new AppStore(createClient(mock2));
    await main.init();
    const m = mount(App, { target: host, props: { store: main, copy: async () => {} } });
    await settle();
    expect(host.querySelector('.sidebar')).not.toBeNull();
    expect(host.querySelector('#editor-root')).toBeNull();
    expect(main.editor).toBeUndefined();
    await unmount(m);
  });

  it('the editor window never loads scripts or calls main-only methods', async () => {
    const mock = new MockBackend();
    await editorStore(mock, 's3');
    const methods = new Set(mock.calls.map((c) => c.method));
    for (const forbidden of ['scripts.list', 'scripts.get', 'settings.update']) expect(methods.has(forbidden)).toBe(false);
    expect(methods.has('editor.open')).toBe(true);
    expect(calls(mock, 'editor.open')[0].params).toEqual({ id: null }); // the host requires the id key
  });
});

describe('main window opens the editor window', () => {
  let mock: MockBackend;
  let main: AppStore;
  beforeEach(async () => {
    mock = new MockBackend();
    main = new AppStore(createClient(mock));
    await main.init();
  });

  it('New and Edit call editor.openWindow and no longer touch a route', async () => {
    await main.openNew();
    expect(calls(mock, 'editor.openWindow')[0].params).toEqual({});
    main.select('s3');
    await mock.call('editor.open', {}).catch(() => {}); // host-side session closed below
    await main.openEditor('s3');
    expect(calls(mock, 'editor.openWindow').at(-1)?.params).toEqual({ id: 's3' });
    expect(main.route).toEqual({ name: 'scripts' });
    expect(calls(mock, 'editor.open').length).toBe(1); // only my probe: main never opens a session itself
  });

  it('a second open reports "Editor already open"', async () => {
    await main.openNew();
    await main.openEditor('s3');
    expect(main.error).toBe('Editor already open');
  });

  it('refreshes the list when the host reports saved changes', async () => {
    const before = calls(mock, 'scripts.list').length;
    mock.push('scripts.changed', { rev: 5 });
    await settle();
    expect(calls(mock, 'scripts.list').length).toBe(before + 1);
  });

  it('toasts a recovered draft path from the editor window events', () => {
    mock.push('editor.windowClosed', { gen: 1, saved: false, forced: true, recoveredPath: 'C:\\r\\a.json' });
    expect(main.error).toContain('C:\\r\\a.json');
    main.error = '';
    mock.push('editor.windowFailed', { gen: 1, stage: 'renderer', message: 'gone' });
    expect(main.error).toContain('gone');
  });

  it('the in-main editor route is gone', () => {
    expect(storeSrc).not.toMatch(/name: 'editor'/);
    expect(storeSrc).not.toContain('pendingRoute');
    expect(appSrc).not.toContain('EditorScreen');
    expect(typesSrc).not.toContain("'editor.close'");
  });
});

describe('measure handshake', () => {
  let mock: MockBackend;
  let api: Client;
  let root: HTMLElement;
  let size = { w: 900, h: 500 };
  const make = () => {
    root = document.createElement('div');
    root.id = 'editor-root';
    root.getBoundingClientRect = () => ({ top: 0, left: 0, right: size.w, bottom: size.h, width: size.w, height: size.h, x: 0, y: 0, toJSON() {} });
    document.body.replaceChildren(root);
    return new MeasureController(api, { root: () => root, ready: async () => {}, frames: async () => {}, fonts: async () => {} });
  };
  beforeEach(() => { mock = new MockBackend(); api = createClient(mock); size = { w: 900.2, h: 500.4 }; });

  it('measures without any animation frame (a hidden window never runs them) and reports painted after the ack', async () => {
    root = document.createElement('div');
    root.id = 'editor-root';
    root.getBoundingClientRect = () => ({ top: 0, left: 0, right: 900, bottom: 500, width: 900, height: 500, x: 0, y: 0, toJSON() {} });
    document.body.replaceChildren(root);
    const raf = vi.spyOn(window, 'requestAnimationFrame').mockImplementation(() => 0);
    const m = new MeasureController(api, { root: () => root, ready: async () => {}, fonts: async () => {}, paint: async () => {} });
    await m.request(3);
    expect(raf).not.toHaveBeenCalled();
    raf.mockRestore();
    const order = mock.calls.map((c) => c.method).filter((n) => ['editor.measured', 'editor.measureAck', 'editor.painted'].includes(n));
    expect(order).toEqual(['editor.measured', 'editor.measureAck', 'editor.painted']);
  });

  it('starts in measure mode, reports ceil CSS px once, and shows + acks on {next:show}', async () => {
    const m = make();
    expect(document.documentElement.dataset.measure).toBe('');
    await m.request(7);
    expect(calls(mock, 'editor.measured')).toHaveLength(1);
    expect(calls(mock, 'editor.measured')[0].params).toEqual({ token: 7, cssW: 901, cssH: 501, dpr: 1 });
    expect(calls(mock, 'editor.measureAck')[0].params).toEqual({ token: 7 });
    expect(document.documentElement.dataset.measure).toBeUndefined();
  });

  it('remeasure sets the width, measures again with the NEW token, then acks that token', async () => {
    const replies = [{ next: 'remeasure', token: 8, cssW: 700 }, { next: 'show' }];
    mock.call = async (method: string, params: unknown) => {
      mock.calls.push({ method, params });
      if (method === 'editor.measured') return replies.shift();
      return {};
    };
    const m = make();
    const p = m.request(7);
    await p;
    const ms = calls(mock, 'editor.measured').map((c) => (c.params as { token: number }).token);
    expect(ms).toEqual([7, 8]);
    expect(calls(mock, 'editor.measureAck').map((c) => c.params)).toEqual([{ token: 8 }]);
    expect(document.documentElement.style.getPropertyValue('--measure-w')).toBe('');
  });

  it('measureAbort leaves measure mode and acks; a measurement error leaves it too', async () => {
    const m = make();
    await m.abort();
    expect(document.documentElement.dataset.measure).toBeUndefined();
    expect(calls(mock, 'editor.measureAck')).toHaveLength(1);
    const m2 = make();
    mock.call = async (method: string, params: unknown) => {
      mock.calls.push({ method, params });
      if (method === 'editor.measured') throw new Error('boom');
      return {};
    };
    await m2.request(2);
    expect(document.documentElement.dataset.measure).toBeUndefined();
  });

  it('a newer token supersedes a measurement in progress (old reply ignored)', async () => {
    let release!: (v: unknown) => void;
    let n = 0;
    mock.call = async (method: string, params: unknown) => {
      mock.calls.push({ method, params });
      if (method === 'editor.measured' && n++ === 0) return new Promise((r) => { release = r; });
      return method === 'editor.measured' ? { next: 'show' } : {};
    };
    const m = make();
    const first = m.request(1);
    await vi.waitFor(() => expect(calls(mock, 'editor.measured')).toHaveLength(1));
    const second = m.request(2);
    await second;
    release({ next: 'remeasure', token: 9, cssW: 700 });
    await first;
    const acks = calls(mock, 'editor.measureAck').map((c) => (c.params as { token: number }).token);
    expect(acks).toEqual([2]);
    expect(calls(mock, 'editor.measured').map((c) => (c.params as { token: number }).token)).toEqual([1, 2]);
  });

  it('a superseded request whose measured call REJECTS does not clear measure mode or ack for the newer token', async () => {
    let rejectFirst!: (e: unknown) => void;
    let n = 0;
    mock.call = async (method: string, params: unknown) => {
      mock.calls.push({ method, params });
      if (method === 'editor.measured') {
        if (n++ === 0) return new Promise((_, rej) => { rejectFirst = rej; });
        return new Promise(() => {}); // the newer measurement is still pending
      }
      return {};
    };
    const m = make();
    const first = m.request(1);
    await vi.waitFor(() => expect(calls(mock, 'editor.measured')).toHaveLength(1));
    const second = m.request(2);
    await vi.waitFor(() => expect(calls(mock, 'editor.measured')).toHaveLength(2));
    rejectFirst(new Error('host dropped the old token'));
    await first;
    expect(document.documentElement.dataset.measure).toBe('');
    expect(m.active).toBe(true);
    expect(calls(mock, 'editor.measureAck')).toHaveLength(0);
    void second;
  });

  it('an abort during the readiness wait invalidates it: no measurement and no further ack', async () => {
    let ready!: () => void;
    root = document.createElement('div');
    root.getBoundingClientRect = () => ({ top: 0, left: 0, right: 900, bottom: 500, width: 900, height: 500, x: 0, y: 0, toJSON() {} });
    document.body.replaceChildren(root);
    const m = new MeasureController(api, { root: () => root, ready: () => new Promise<void>((r) => { ready = r; }), frames: async () => {}, fonts: async () => {} });
    const p = m.request(3);
    await m.abort();
    ready();
    await p;
    expect(calls(mock, 'editor.measured')).toHaveLength(0);
    expect(calls(mock, 'editor.measureAck')).toHaveLength(1);
    expect(document.documentElement.dataset.measure).toBeUndefined();
  });

  it('an abort during an in-flight measured RPC ignores its reply (no remeasure, no second ack)', async () => {
    let reply!: (v: unknown) => void;
    mock.call = async (method: string, params: unknown) => {
      mock.calls.push({ method, params });
      return method === 'editor.measured' ? new Promise((r) => { reply = r; }) : {};
    };
    const m = make();
    const p = m.request(4);
    await vi.waitFor(() => expect(calls(mock, 'editor.measured')).toHaveLength(1));
    await m.abort();
    reply({ next: 'remeasure', token: 5, cssW: 700 });
    await p;
    expect(calls(mock, 'editor.measured')).toHaveLength(1);
    expect(calls(mock, 'editor.measureAck')).toHaveLength(1);
    expect(document.documentElement.style.getPropertyValue('--measure-w')).toBe('');
  });

  it('clamps reported sizes to the host range 1..8192', async () => {
    size = { w: 99999, h: 0 };
    const m = make();
    await m.request(1);
    expect(calls(mock, 'editor.measured')[0].params).toMatchObject({ cssW: 8192, cssH: 1 });
  });
});

describe('scrollbar metrics', () => {
  it('publishes the scrollbar width and the matching column weights (0 for overlay scrollbars)', () => {
    expect(scrollbarWidth()).toBe(0);
    applyScrollbarMetrics();
    const st = document.documentElement.style;
    expect(st.getPropertyValue('--sbw')).toBe('0px');
    expect(st.getPropertyValue('--f1')).toBe('300fr');
    expect(st.getPropertyValue('--f2')).toBe('340fr');
    expect(st.getPropertyValue('--f3')).toBe('260fr');
  });
});

describe('close guard', () => {
  let mock: MockBackend;
  let store: AppStore;
  beforeEach(async () => {
    mock = new MockBackend();
    store = await editorStore(mock, 's3');
  });
  const edit = () => { store.editor.script!.name = 'Renamed'; store.editor.markDirty(); };

  it('a clean editor sends discard and never saves', async () => {
    await store.editor.guard();
    expect(decisions(mock)).toEqual(['discard']);
    expect(calls(mock, 'editor.save')).toHaveLength(0);
  });

  it('dirty: exactly one save with snapshotRev, then decision saved (never editor.close)', async () => {
    edit();
    await store.editor.guard();
    const saves = calls(mock, 'editor.save');
    expect(saves).toHaveLength(1);
    expect(saves[0].params).toMatchObject({ snapshotRev: 1 });
    expect(decisions(mock)).toEqual(['saved']);
    expect(calls(mock, 'editor.close')).toHaveLength(0);
    expect(mock.calls.map((c) => c.method).indexOf('editor.save')).toBeLessThan(mock.calls.map((c) => c.method).indexOf('editor.closeDecision'));
  });

  it('a second close request while the guard runs does not save twice', async () => {
    edit();
    const a = store.editor.guard();
    const b = store.editor.guard();
    await Promise.all([a, b]);
    expect(calls(mock, 'editor.save')).toHaveLength(1);
    expect(decisions(mock)).toHaveLength(1);
  });

  it('editor.closeRequested runs the guard; other sessions are ignored', async () => {
    edit();
    mock.push('editor.closeRequested', { gen: 1, sessionId: 'someone-else', reason: 'user' });
    await settle();
    expect(calls(mock, 'editor.save')).toHaveLength(0);
    mock.push('editor.closeRequested', { gen: 1, sessionId: store.editor.sessionId!, reason: 'user' });
    await vi.waitFor(() => expect(decisions(mock)).toEqual(['saved']));
  });

  it('closeRequested for another gen is ignored', async () => {
    history.replaceState(null, '', '/?window=editor&gen=7');
    mock.push('editor.closeRequested', { gen: 6, reason: 'user' });
    await settle();
    expect(decisions(mock)).toEqual([]);
    mock.push('editor.closeRequested', { gen: 7, reason: 'user' });
    await vi.waitFor(() => expect(decisions(mock)).toEqual(['discard']));
  });

  it('the Close button asks the host (requestClose) instead of releasing the session', async () => {
    await store.editor.requestClose();
    expect(calls(mock, 'editor.requestClose')).toHaveLength(1);
    await vi.waitFor(() => expect(decisions(mock)).toEqual(['discard']));
  });

  it('hotkey conflict: dialog, No cancels (stay open); Yes overrides then saved', async () => {
    store.editor.script!.toggle_hotkey = { modifiers: [], key: 'f5' };
    store.editor.markDirty();
    let g = store.editor.guard();
    await vi.waitFor(() => expect(store.editor.conflicts).not.toBeNull());
    store.editor.resolveConflict(false);
    await g;
    expect(decisions(mock)).toEqual(['cancel']);
    expect(store.editor.sessionId).not.toBeNull();
    g = store.editor.guard();
    await vi.waitFor(() => expect(store.editor.conflicts).not.toBeNull());
    store.editor.resolveConflict(true);
    await g;
    expect(decisions(mock)).toEqual(['cancel', 'saved']);
    const overrides = calls(mock, 'editor.save').filter((c) => (c.params as { overrideConflicts?: boolean }).overrideConflicts);
    expect(overrides).toHaveLength(1);
  });

  it('a failing save keeps the draft: keep editing cancels, discard discards', async () => {
    mock.failSaveFor.add('s3');
    edit();
    let g = store.editor.guard();
    await vi.waitFor(() => expect(store.editor.discardPrompt).not.toBeNull());
    store.editor.keepEditing();
    await g;
    expect(decisions(mock)).toEqual(['cancel']);
    expect(store.editor.script?.name).toBe('Renamed');
    g = store.editor.guard();
    await vi.waitFor(() => expect(store.editor.discardPrompt).not.toBeNull());
    store.editor.discardAndClose();
    await g;
    expect(decisions(mock)).toEqual(['cancel', 'discard']);
  });

  it('an invalid name is reported by the host (NAME_TAKEN) and prompts, never discards silently', async () => {
    store.editor.script!.name = 'Defense buff';
    store.editor.markDirty();
    const g = store.editor.guard();
    await vi.waitFor(() => expect(store.editor.discardPrompt).toContain('name taken'));
    expect(store.editor.indicator.text).toBe('Not saved: name taken');
    store.editor.keepEditing();
    await g;
    expect(decisions(mock)).toEqual(['cancel']);
  });

  it('app-closing: flush with ignore_conflicts, no dialogs, no guardActive, saved', async () => {
    store.editor.script!.toggle_hotkey = { modifiers: [], key: 'f5' };
    store.editor.markDirty();
    mock.push('editor.closeRequested', { gen: 1, sessionId: store.editor.sessionId!, reason: 'app-closing' });
    await vi.waitFor(() => expect(decisions(mock)).toEqual(['saved']));
    expect(calls(mock, 'editor.save')[0].params).toMatchObject({ ignore_conflicts: true });
    expect(calls(mock, 'editor.guardActive')).toHaveLength(0);
    expect(calls(mock, 'app.closingReady')).toHaveLength(0);
  });

  it('app-closing with an unsaveable draft sends cancel (the host recovers it) without prompting', async () => {
    mock.failSaveFor.add('s3');
    edit();
    mock.push('editor.closeRequested', { gen: 1, sessionId: store.editor.sessionId!, reason: 'app-closing' });
    await vi.waitFor(() => expect(decisions(mock)).toEqual(['cancel']));
    expect(store.editor.discardPrompt).toBeNull();
  });

  it('the editor window never sends app.closingReady', async () => {
    mock.push('app.closing', { deadline_ms: 1500 });
    await settle();
    expect(calls(mock, 'app.closingReady')).toHaveLength(0);
  });
});

describe('guardActive pings', () => {
  it('ping at dialog open and every second while it stays open; none otherwise; none after the decision', async () => {
    vi.useFakeTimers();
    const mock = new MockBackend();
    const store = await editorStore(mock, 's3');
    mock.failSaveFor.add('s3');
    store.editor.script!.name = 'X';
    store.editor.markDirty();
    const g = store.editor.guard();
    await vi.advanceTimersByTimeAsync(0);
    expect(calls(mock, 'editor.guardActive')).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(GUARD_PING_MS * 3);
    expect(calls(mock, 'editor.guardActive')).toHaveLength(4);
    store.editor.keepEditing();
    await g;
    await vi.advanceTimersByTimeAsync(GUARD_PING_MS * 3);
    expect(calls(mock, 'editor.guardActive')).toHaveLength(4);
  });

  it('no ping while no dialog is open (a plain save)', async () => {
    vi.useFakeTimers();
    const mock = new MockBackend();
    const store = await editorStore(mock, 's3');
    store.editor.markDirty();
    await store.editor.guard();
    await vi.advanceTimersByTimeAsync(GUARD_PING_MS * 3);
    expect(calls(mock, 'editor.guardActive')).toHaveLength(0);
  });
});

describe('checkpoints', () => {
  it('sender: 500 ms trailing debounce, 2 s max-wait, one in flight', async () => {
    vi.useFakeTimers();
    const send = vi.fn(() => Promise.resolve());
    const c = new CheckpointSender(send);
    c.touch();
    await vi.advanceTimersByTimeAsync(499);
    expect(send).toHaveBeenCalledTimes(0);
    await vi.advanceTimersByTimeAsync(2);
    expect(send).toHaveBeenCalledTimes(1);
    for (let i = 0; i < 12; i++) { c.touch(); await vi.advanceTimersByTimeAsync(300); }
    expect(send.mock.calls.length).toBeGreaterThanOrEqual(2); // continuous typing still flushes at the max wait
    expect(send.mock.calls.length).toBeLessThanOrEqual(3);
    let release!: () => void;
    const slow = vi.fn(() => new Promise<void>((r) => { release = r; }));
    const d = new CheckpointSender(slow);
    d.touch();
    await vi.advanceTimersByTimeAsync(600);
    d.touch();
    await vi.advanceTimersByTimeAsync(600);
    expect(slow).toHaveBeenCalledTimes(1);
    release();
    await vi.advanceTimersByTimeAsync(600);
    expect(slow).toHaveBeenCalledTimes(2);
  });

  it('edits send monotonically increasing revisions; applied:false is a no-op', async () => {
    vi.useFakeTimers();
    const mock = new MockBackend();
    const store = await editorStore(mock, 's3');
    store.editor.markDirty();
    await vi.advanceTimersByTimeAsync(600);
    store.editor.markDirty();
    store.editor.markDirty();
    await vi.advanceTimersByTimeAsync(600);
    expect(mock.checkpoints.map((c) => c.revision)).toEqual([1, 3]);
    expect(mock.checkpoints.every((c) => c.applied)).toBe(true);
    expect(calls(mock, 'editor.checkpoint')[0].params).toMatchObject({ sessionId: store.editor.sessionId, revision: 1 });
  });

  it('a clean editor sends nothing; a save clears the pending checkpoint', async () => {
    vi.useFakeTimers();
    const mock = new MockBackend();
    const store = await editorStore(mock, 's3');
    await vi.advanceTimersByTimeAsync(3000);
    expect(calls(mock, 'editor.checkpoint')).toHaveLength(0);
    store.editor.markDirty();
    await store.editor.guard();
    await vi.advanceTimersByTimeAsync(3000);
    expect(calls(mock, 'editor.checkpoint')).toHaveLength(0);
  });

  it('revisions continue from the revision editor.open returned and the save carries snapshotRev', async () => {
    vi.useFakeTimers();
    const mock = new MockBackend();
    const orig = mock.call.bind(mock);
    mock.call = async (m: string, p: unknown) => {
      const r = await orig(m, p);
      return m === 'editor.open' ? { ...(r as object), revision: 41 } : r;
    };
    const store = await editorStore(mock, 's3');
    store.editor.markDirty();
    await vi.advanceTimersByTimeAsync(600);
    expect(mock.checkpoints.map((c) => c.revision)).toEqual([42]);
    await store.editor.guard();
    expect(calls(mock, 'editor.save')[0].params).toMatchObject({ snapshotRev: 42 });
  });

  it('a draft over 1 MiB is not sent', async () => {
    vi.useFakeTimers();
    const mock = new MockBackend();
    const store = await editorStore(mock, 's3');
    store.editor.script!.name = 'x'.repeat(MAX_DRAFT_BYTES + 1);
    store.editor.markDirty();
    await vi.advanceTimersByTimeAsync(3000);
    expect(calls(mock, 'editor.checkpoint')).toHaveLength(0);
    expect(draftBytes({ a: 'é' })).toBe(10);
  });
});
