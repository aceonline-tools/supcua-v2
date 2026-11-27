import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createLoader, delayCalls } from './loader';

let t = 0;
const now = () => t;
const advance = (ms: number) => { t += ms; vi.advanceTimersByTime(ms); };

function boot() {
  document.body.innerHTML = '<div id="boot"><p id="boot-st">Starting…</p></div><div id="app"></div>';
}
const el = () => document.getElementById('boot');
const text = () => document.getElementById('boot-st')!.textContent;

beforeEach(() => { vi.useFakeTimers(); t = 0; boot(); });
afterEach(() => { vi.useRealTimers(); });

describe('status line', () => {
  it('is hidden before 700ms, then visible, and follows the stages', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    l.stage('interface');
    expect(text()).toBe('Loading interface…');
    advance(699);
    expect(el()!.dataset.status).toBeUndefined();
    advance(1);
    expect(el()!.dataset.status).toBe('visible');
    l.stage('scripts');
    expect(text()).toBe('Loading scripts…');
  });

  it('counts from navigation start: a bundle that runs late shows the status at once', () => {
    t = 2500;
    createLoader({ now, reducedMotion: () => false });
    advance(0);
    expect(el()!.dataset.status).toBe('visible');
  });
});

describe('hand-off', () => {
  it('waits for the data promise, then fades 150ms before removing', async () => {
    const l = createLoader({ now, reducedMotion: () => false });
    let resolve!: () => void;
    const data = new Promise<void>((r) => { resolve = r; });
    void data.then(() => l.done());
    advance(1000);
    expect(el()).not.toBeNull();
    expect(el()!.classList.contains('out')).toBe(false);
    resolve();
    await Promise.resolve();
    expect(el()!.classList.contains('out')).toBe(true);
    advance(149);
    expect(el()).not.toBeNull();
    advance(1);
    expect(el()).toBeNull();
  });

  it('fast start (<100ms): removed at once, no fade frame', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    advance(99);
    l.done();
    expect(el()).toBeNull();
  });

  it('exactly 100ms still fades', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    advance(100);
    l.done();
    expect(el()!.classList.contains('out')).toBe(true);
  });

  it('reduced motion: instant swap even on a slow start', () => {
    const l = createLoader({ now, reducedMotion: () => true });
    advance(2000);
    l.done();
    expect(el()).toBeNull();
  });

  it('done is idempotent and later stages are ignored', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    advance(500);
    l.done();
    l.done();
    l.stage('scripts');
    expect(text()).toBe('Starting…');
    advance(150);
    expect(el()).toBeNull();
  });

  it('failsafe: a hung initial load still removes the screen', () => {
    createLoader({ now, reducedMotion: () => false, failsafeMs: 10000 });
    advance(9999);
    expect(el()).not.toBeNull();
    advance(1);
    advance(150);
    expect(el()).toBeNull();
  });

  it('is a no-op without the inline markup', () => {
    document.body.innerHTML = '';
    const l = createLoader({ now });
    expect(() => { l.stage('scripts'); l.done(); }).not.toThrow();
  });
});

describe('delayCalls (dev hook)', () => {
  it('delays calls by ms, passes through at 0', async () => {
    const inner = { call: vi.fn(async (m: string) => m), on: vi.fn() };
    expect(delayCalls(inner as never, 0)).toBe(inner);
    const c = delayCalls(inner as never, 300) as typeof inner;
    const p = c.call('x');
    advance(299);
    expect(inner.call).not.toHaveBeenCalled();
    advance(1);
    await expect(p).resolves.toBe('x');
  });
});
