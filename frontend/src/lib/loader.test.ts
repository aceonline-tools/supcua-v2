import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createLoader, delayCalls, trackFirstCalls } from './loader';

let t = 0;
const now = () => t;
const advance = (ms: number) => { t += ms; vi.advanceTimersByTime(ms); };

function boot() {
  document.body.innerHTML = '<div id="boot"><p id="boot-st">Starting…</p><button id="boot-go" hidden>Continue</button></div><div id="app" inert aria-busy="true"><h1>Scripts</h1><button>x</button></div>';
}
const el = () => document.getElementById('boot');
const app = () => document.getElementById('app')!;
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

  it('slow start: after 10s the status turns calm and the app is NOT revealed', () => {
    createLoader({ now, reducedMotion: () => false });
    advance(9999);
    expect(text()).toBe('Starting…');
    advance(1);
    expect(text()).toBe('Still loading… this is taking longer than usual');
    advance(60000);
    expect(el()).not.toBeNull();
    expect(app().hasAttribute('inert')).toBe(true);
  });

  it('is a no-op without the inline markup', () => {
    document.body.innerHTML = '';
    const l = createLoader({ now });
    expect(() => { l.stage('scripts'); l.done(); }).not.toThrow();
  });
});

describe('inert / aria-busy', () => {
  it('app root is inert+busy until the hand-off starts, then released (fade path)', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    advance(500);
    expect(app().hasAttribute('inert')).toBe(true);
    expect(app().getAttribute('aria-busy')).toBe('true');
    l.done();
    expect(app().hasAttribute('inert')).toBe(false);
    expect(app().hasAttribute('aria-busy')).toBe(false);
  });

  it('released on the instant paths too', () => {
    createLoader({ now, reducedMotion: () => true }).done();
    expect(app().hasAttribute('inert')).toBe(false);
  });
});

describe('error state', () => {
  it('shows the message, keeps the app inert, and reveals it only on Continue', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    advance(300);
    l.fail('boom');
    l.done();
    expect(text()).toBe('boom');
    expect(el()!.classList.contains('err')).toBe(true);
    expect(app().hasAttribute('inert')).toBe(true);
    const go = document.getElementById('boot-go') as HTMLButtonElement;
    expect(go.hidden).toBe(false);
    go.click();
    expect(app().hasAttribute('inert')).toBe(false);
    advance(150);
    expect(el()).toBeNull();
  });

  it('after Continue, focus moves into #app (the heading), not lost', () => {
    const l = createLoader({ now, reducedMotion: () => false });
    advance(300);
    l.fail('boom');
    const go = document.getElementById('boot-go') as HTMLButtonElement;
    expect(document.activeElement).toBe(go);
    go.click();
    expect(app().contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(app().querySelector('h1'));
  });

  it('a normal hand-off does not steal focus', () => {
    const outside = document.createElement('input');
    document.body.appendChild(outside);
    outside.focus();
    const l = createLoader({ now, reducedMotion: () => false });
    advance(300);
    l.done();
    expect(document.activeElement).toBe(outside);
  });
});

describe('trackFirstCalls', () => {
  it('waits for every first call to settle and reports a rejection', async () => {
    let rejectSettings!: (e: Error) => void;
    let resolveScripts!: () => void;
    const inner = {
      call: vi.fn((m: string) => m === 'settings.get' ? new Promise((_, rj) => { rejectSettings = rj; }) : m === 'scripts.list' ? new Promise<void>((r) => { resolveScripts = r; }) : Promise.resolve()),
    };
    const { client, loads } = trackFirstCalls(inner as never, ['settings.get', 'scripts.list']) as { client: typeof inner; loads: { settled(): Promise<string | null> } };
    void client.call('settings.get'); void client.call('scripts.list'); void client.call('other');
    let out: string | null | undefined;
    void loads.settled().then((v) => { out = v; });
    rejectSettings(new Error('settings failed'));
    await Promise.resolve(); await Promise.resolve();
    expect(out).toBeUndefined();
    resolveScripts();
    await vi.advanceTimersByTimeAsync(0);
    expect(out).toBe('settings failed');
  });

  it('null when all first loads succeed', async () => {
    const { client, loads } = trackFirstCalls({ call: async () => 1 } as never, ['a']) as unknown as { client: { call(m: string): Promise<number> }; loads: { settled(): Promise<string | null> } };
    await client.call('a');
    expect(await loads.settled()).toBeNull();
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
