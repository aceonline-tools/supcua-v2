import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const calls: string[] = [];
let bootMode = 'licensed';
let scriptsResult: () => Promise<unknown> = async () => ({});
let finishInit!: () => void;

vi.mock('svelte', () => ({ mount: vi.fn(() => calls.push('mount')), tick: () => Promise.resolve() }));
vi.mock('./app.css', () => ({}));
vi.mock('./App.svelte', () => ({ default: {} }));
vi.mock('./api/bootstrap', () => ({ createAppClient: async () => ({ call: (m: string) => { calls.push(m); return m === 'scripts.list' ? scriptsResult() : Promise.resolve(); }, on: () => () => {} }) }));
vi.mock('./lib/store.svelte', () => ({
  AppStore: class {
    boot = { mode: bootMode };
    constructor(private c: { call(m: string): Promise<unknown> }) {}
    init() { void this.c.call('scripts.list').catch(() => {}); return new Promise<void>((r) => { finishInit = r; }); }
  },
}));

beforeEach(() => {
  vi.useFakeTimers();
  calls.length = 0;
  scriptsResult = async () => ({});
  bootMode = 'licensed';
  document.body.innerHTML = '<div id="boot"><p id="boot-st"></p></div><div id="app" inert aria-busy="true"></div><button id="boot-go" hidden></button>';
  vi.resetModules();
});
afterEach(() => vi.useRealTimers());

it('reports app.mounted (host load-timeout disarm) at mount, while the screen waits for the first data', async () => {
  await import('./main');
  expect(calls).toContain('mount');
  expect(calls).toContain('app.mounted');
  expect(document.getElementById('boot-st')!.textContent).toBe('Loading scripts…');
  await vi.advanceTimersByTimeAsync(500);
  expect(document.getElementById('boot')).not.toBeNull();
  finishInit();
  await vi.advanceTimersByTimeAsync(0);
  await vi.advanceTimersByTimeAsync(200);
  expect(document.getElementById('boot')).toBeNull();
});

it('a host that never answers keeps the loader (calm slow status), never reveals an empty app; app.mounted was still sent', async () => {
  await import('./main');
  await vi.advanceTimersByTimeAsync(60_000);
  expect(document.getElementById('boot')).not.toBeNull();
  expect(document.getElementById('boot-st')!.textContent).toMatch(/Still loading/);
  expect(document.getElementById('app')!.hasAttribute('inert')).toBe(true);
  expect(calls).toContain('app.mounted');
});

it('init resolving early (settings failed) while scripts.list is pending does not hand off', async () => {
  let rejectScripts!: (e: Error) => void;
  scriptsResult = () => new Promise((_, rj) => { rejectScripts = rj; });
  await import('./main');
  finishInit();
  await vi.advanceTimersByTimeAsync(500);
  expect(document.getElementById('boot')).not.toBeNull();
  expect(document.getElementById('boot')!.classList.contains('err')).toBe(false);
  rejectScripts(new Error('scripts exploded'));
  await vi.advanceTimersByTimeAsync(10);
  expect(document.getElementById('boot')!.classList.contains('err')).toBe(true);
  expect(document.getElementById('boot-st')!.textContent).toContain('scripts exploded');
  expect(document.getElementById('app')!.hasAttribute('inert')).toBe(true);
});

it('an unlicensed install (scripts/settings reject by design) hands off to the licence screen instead of the error state', async () => {
  bootMode = 'notLicensed';
  scriptsResult = () => Promise.reject(new Error('not licensed'));
  await import('./main');
  finishInit();
  await vi.advanceTimersByTimeAsync(300);
  expect(document.getElementById('boot')).toBeNull();
  expect(calls).toContain('app.log');
});
