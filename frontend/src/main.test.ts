import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const calls: string[] = [];
let finishInit!: () => void;

vi.mock('svelte', () => ({ mount: vi.fn(() => calls.push('mount')), tick: () => Promise.resolve() }));
vi.mock('./app.css', () => ({}));
vi.mock('./App.svelte', () => ({ default: {} }));
vi.mock('./api/bootstrap', () => ({ createAppClient: async () => ({ call: async (m: string) => { calls.push(m); }, on: () => () => {} }) }));
vi.mock('./lib/store.svelte', () => ({
  AppStore: class { init() { return new Promise<void>((r) => { finishInit = r; }); } },
}));

beforeEach(() => {
  vi.useFakeTimers();
  calls.length = 0;
  document.body.innerHTML = '<div id="boot"><p id="boot-st"></p></div><div id="app"></div>';
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

it('a host that never answers cannot leave the loader up forever (failsafe), and app.mounted was still sent', async () => {
  await import('./main');
  await vi.advanceTimersByTimeAsync(10_200);
  expect(document.getElementById('boot')).toBeNull();
  expect(calls).toContain('app.mounted');
});
