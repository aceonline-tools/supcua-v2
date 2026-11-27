import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from './store.svelte';
import type { OverlayStyleDTO } from '../api/types';

let mock: MockBackend;
let store: AppStore;
type Gate = { params: OverlayStyleDTO | undefined; method: string; release: (fail?: boolean) => void };
let gates: Gate[];

beforeEach(async () => {
  vi.useFakeTimers();
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  gates = [];
  const orig = mock.call.bind(mock);
  mock.call = (m: string, p: unknown) => {
    if (m !== 'overlay.setStyle' && m !== 'overlay.resetStyle') return orig(m, p);
    return new Promise((resolve, reject) => {
      gates.push({
        method: m, params: p as OverlayStyleDTO | undefined,
        release: (fail) => (fail ? reject(new ApiError({ code: 'SAVE_FAILED', message: 'Could not save' })) : orig(m, p).then(resolve, reject)),
      });
    });
  };
});
afterEach(() => vi.useRealTimers());

const st = () => store.settings!.overlay_style;
const tick = async (ms = 130) => { await vi.advanceTimersByTimeAsync(ms); };

describe('overlay style edit races', () => {
  it('A in flight, B typed, A resolves before B\'s timer: B keeps its own value and sends A+B', async () => {
    store.setOverlayStyle({ radius: 10 });
    await tick();
    expect(gates).toHaveLength(1);
    store.setOverlayStyle({ width: 300 });
    gates[0].release();
    await tick(10);
    expect(st()).toMatchObject({ radius: 10, width: 300 });
    await tick();
    expect(gates).toHaveLength(2);
    expect(gates[1].params).toMatchObject({ radius: 10, width: 300 });
    gates[1].release();
    await tick(10);
    expect(st()).toMatchObject({ radius: 10, width: 300 });
  });

  it('A fails while B is pending: no rollback over B; B failing rolls back to the confirmed style', async () => {
    store.setOverlayStyle({ radius: 10 });
    await tick();
    store.setOverlayStyle({ width: 300 });
    gates[0].release(true);
    await tick(10);
    expect(st()).toMatchObject({ width: 300 });
    await tick();
    expect(gates[1].params).toMatchObject({ radius: 0, width: 300 });
    gates[1].release(true);
    await tick(10);
    expect(st()).toMatchObject({ radius: 0, width: 136 });
  });

  it('Reset while an edit is pending cancels the edit and wins', async () => {
    store.setOverlayStyle({ radius: 12 });
    const reset = store.resetOverlayStyle();
    expect(st()).toMatchObject({ radius: 0, width: 136 });
    await tick();
    expect(gates).toHaveLength(1);
    expect(gates[0].method).toBe('overlay.resetStyle');
    gates[0].release();
    await reset;
    expect(st()).toMatchObject({ radius: 0, background: '#404040' });
  });

  it('an edit made while Reset is in flight survives the Reset response', async () => {
    store.setOverlayStyle({ radius: 12 });
    await tick();
    gates[0].release();
    await tick(10);
    void store.resetOverlayStyle();
    await tick(1);
    store.setOverlayStyle({ width: 250 });
    gates[1].release();
    await tick(10);
    expect(st().width).toBe(250);
    await tick();
    expect(gates[2].params).toMatchObject({ width: 250, radius: 0 });
  });

  it('a stale settings.get does not overwrite a newer overlay edit', async () => {
    const orig = mock.call.bind(mock);
    let resolveGet: (() => void) | undefined;
    mock.call = (m: string, p: unknown) => (m === 'settings.get'
      ? orig(m, p).then((r) => new Promise((res) => { resolveGet = () => res(r); }))
      : orig(m, p));
    const load = store.loadSettings();
    await tick(1);
    store.setOverlayStyle({ radius: 9 });
    resolveGet!();
    await load;
    expect(st().radius).toBe(9);
  });

  const seed = () => (mock as unknown as { seed: { settings: { overlay_style: OverlayStyleDTO } } }).seed.settings.overlay_style;
  const drag = async (x: number, y: number) => {
    seed().x = x; seed().y = y;
    mock.push('settings.changed', { fields: ['overlay_style'] });
    await tick(1);
  };

  it('a settings fetch started during an edit cannot overwrite the edit\'s completed result', async () => {
    const orig = mock.call.bind(mock);
    let hold: (() => void) | undefined;
    let first = true;
    mock.call = (m: string, p: unknown) => {
      if (m === 'settings.get' && first) { // snapshot taken NOW (the backend's previous value), delivered late
        first = false;
        return orig(m, p).then((r) => new Promise((res) => { hold = () => res(r); }));
      }
      return orig(m, p);
    };
    store.setOverlayStyle({ radius: 10 });
    await tick();
    expect(gates).toHaveLength(1);
    const load = store.loadSettings();
    await tick(1);
    gates[0].release(); // A completes; the backend now has radius 10
    await tick(10);
    expect(st().radius).toBe(10);
    hold!(); // the old fetch lands after A's confirmation
    await load;
    await tick(10);
    expect(st().radius).toBe(10);
  });

  it('a pending colour edit + a drag: the sent style has the new colour AND the dragged X/Y', async () => {
    store.setOverlayStyle({ background: '#112233' });
    await drag(640, 360);
    expect(st()).toMatchObject({ background: '#112233', x: 640, y: 360 });
    await tick();
    expect(gates).toHaveLength(1);
    expect(gates[0].params).toMatchObject({ background: '#112233', x: 640, y: 360 });
  });

  it('X/Y typed by the user win over a drag that lands before the debounce fires', async () => {
    store.setOverlayStyle({ x: 111, y: 222 });
    await drag(640, 360);
    expect(st()).toMatchObject({ x: 111, y: 222 });
    await tick();
    expect(gates[0].params).toMatchObject({ x: 111, y: 222 });
  });

  it('a drag that happened before the edit is not reverted by it', async () => {
    await drag(500, 300);
    store.setOverlayStyle({ radius: 8 });
    await tick();
    expect(gates[0].params).toMatchObject({ radius: 8, x: 500, y: 300 });
  });
});
