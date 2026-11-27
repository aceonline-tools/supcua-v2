import { describe, expect, it } from 'vitest';
import { MockHub, type HubHost } from './mockHub';

function setup() {
  const log: string[] = [];
  const host: HubHost = { createEditorWindow: (g: number) => log.push(`create ${g}`), destroyEditorWindow: (g) => log.push(`destroy ${g}`) };
  const hub = new MockHub(host);
  const mainEv: [string, any][] = [];
  const main = hub.transport('main');
  main.subscribe((n, p) => mainEv.push([n, p]));
  const open = async (id: string | null = 's1') => {
    const r = (await main.call('editor.openWindow', id ? { id } : {})) as { gen: number };
    const ed = hub.transport('editor', r.gen);
    const edEv: [string, any][] = [];
    ed.subscribe((n, p) => edEv.push([n, p]));
    return { gen: r.gen, ed, edEv };
  };
  const tick = () => new Promise((r) => setTimeout(r, 5));
  return { hub, main, mainEv, log, open, tick };
}
const code = async (p: Promise<unknown>) => p.then(() => 'ok', (e) => e.code);

describe('MockHub role enforcement (bridge/roles.go)', () => {
  it('main cannot call editor-only methods, editor cannot call main-only or unknown methods', async () => {
    const { main, open } = setup();
    expect(await code(main.call('editor.open', { id: null }))).toBe('FORBIDDEN_ROLE');
    expect(await code(main.call('editor.save', {}))).toBe('FORBIDDEN_ROLE');
    const { ed } = await open();
    for (const m of ['scripts.list', 'scripts.delete', 'settings.update', 'editor.openWindow', 'editor.close', 'overlay.set', 'nope.unknown']) {
      expect(await code(ed.call(m, {})), m).toBe('FORBIDDEN_ROLE');
    }
    expect(await code(ed.call('settings.get', {}))).toBe('ok');
    expect(await code(main.call('settings.get', {}))).toBe('ok');
  });
});

describe('MockHub generations', () => {
  it('opens a window per gen, focuses a second open, rejects a stale gen after close', async () => {
    const { main, log, open } = setup();
    const a = await open();
    expect(a.gen).toBe(1);
    expect(log).toEqual(['create 1']);
    expect(await main.call('editor.openWindow', { id: 's2' })).toEqual({ status: 'focused', gen: 1 });
    expect(log).toEqual(['create 1']);
    const o = (await a.ed.call('editor.open', { id: null })) as { sessionId: string; script: { id: string } };
    expect(o.script.id).toBe('s1');
    await a.ed.call('editor.closeDecision', { sessionId: o.sessionId, decision: 'discard' });
    expect(log).toEqual(['create 1', 'destroy 1']);
    expect(await code(a.ed.call('settings.get', {}))).toBe('STALE_GEN');
    const b = await open('s2');
    expect(b.gen).toBe(2);
    expect(await code(a.ed.call('editor.guardActive', { sessionId: 'x' }))).toBe('STALE_GEN');
  });

  it('cancel keeps the window', async () => {
    const { log, open } = setup();
    const { ed } = await open();
    const o = (await ed.call('editor.open', { id: null })) as { sessionId: string };
    await ed.call('editor.closeDecision', { sessionId: o.sessionId, decision: 'cancel' });
    expect(log).toEqual(['create 1']);
    expect(await code(ed.call('settings.get', {}))).toBe('ok');
  });
});

describe('MockHub strict decode', () => {
  it('editor.open requires the id key as string or null', async () => {
    const { open } = setup();
    const { ed } = await open();
    expect(await code(ed.call('editor.open', {}))).toBe('BAD_REQUEST');
    expect(await code(ed.call('editor.open', { id: 5 }))).toBe('BAD_REQUEST');
    expect(await code(ed.call('editor.open', { id: null }))).toBe('ok');
  });
});

describe('MockHub events per window', () => {
  it('routes editor-only events to the live editor, main-only to main, both to both', async () => {
    const { hub, mainEv, open, tick } = setup();
    const { ed, edEv } = await open();
    await ed.call('editor.windowReady', {});
    await tick();
    expect(edEv.map((e) => e[0])).toEqual(['editor.measureRequest']);
    expect(mainEv.map((e) => e[0])).not.toContain('editor.measureRequest');
    hub.backend.push('state', { x: 1 });
    hub.backend.push('update.available', { tag: 'v9' });
    expect(edEv.map((e) => e[0])).toEqual(['editor.measureRequest', 'state']);
    expect(mainEv.map((e) => e[0])).toEqual(['state', 'update.available']);
  });

  it('closes with editor.windowClosed to main only, and a late editor event is dropped', async () => {
    const { hub, main, mainEv, open } = setup();
    const { ed, edEv } = await open();
    const o = (await ed.call('editor.open', { id: null })) as { sessionId: string };
    await ed.call('editor.closeDecision', { sessionId: o.sessionId, decision: 'saved' });
    expect(mainEv.at(-1)).toEqual(['editor.windowClosed', { gen: 1, saved: true, forced: false }]);
    expect(edEv.some((e) => e[0] === 'editor.windowClosed')).toBe(false);
    hub.backend.push('capture.key', { capture_id: 1, key: 'A' });
    expect(edEv.some((e) => e[0] === 'capture.key')).toBe(false);
    expect((await main.call('state.get', {}) as { editor_open: boolean }).editor_open).toBe(false);
  });

  it('host close request reaches only the editor; failWindow reports to main and frees the slot', async () => {
    const { hub, main, mainEv, log, open, tick } = setup();
    const { ed, edEv } = await open();
    await ed.call('editor.open', { id: null });
    hub.hostCloseRequest();
    await tick();
    expect(edEv.at(-1)?.[0]).toBe('editor.closeRequested');
    expect(mainEv.some((e) => e[0] === 'editor.closeRequested')).toBe(false);
    hub.failWindow('measure', 'boom');
    expect(mainEv.at(-1)).toEqual(['editor.windowFailed', { gen: 1, stage: 'measure', message: 'boom' }]);
    expect(log.at(-1)).toBe('destroy 1');
    expect(await main.call('editor.openWindow', {})).toEqual({ status: 'opening', gen: 2 });
  });
});

describe('MockHub scripts.changed fan-out', () => {
  it('a save in the editor updates main and reaches both windows', async () => {
    const { main, mainEv, open, tick } = setup();
    const { ed, edEv } = await open();
    const o = (await ed.call('editor.open', { id: null })) as { sessionId: string; script: { name: string } };
    await ed.call('editor.save', { sessionId: o.sessionId, script: { ...o.script, name: 'Renamed' } });
    await tick();
    expect(mainEv.some((e) => e[0] === 'scripts.changed')).toBe(true);
    expect(edEv.some((e) => e[0] === 'scripts.changed')).toBe(true);
    const list = (await main.call('scripts.list', {})) as { scripts: { name: string }[] };
    expect(list.scripts.map((s) => s.name)).toContain('Renamed');
  });
});
