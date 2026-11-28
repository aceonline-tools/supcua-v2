import { afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend, defaultSeed, type MockSeed } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import App from '../App.svelte';

let app: ReturnType<typeof mount> | undefined;
afterEach(() => { if (app) unmount(app); app = undefined; });

const tick = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); flushSync(); };

async function settings(seed: Partial<MockSeed>) {
  const host = document.createElement('div');
  document.body.replaceChildren(host);
  const backend = new MockBackend({ ...defaultSeed(), ...seed });
  const store = new AppStore(createClient(backend));
  await store.init();
  store.go({ name: 'settings' });
  app = mount(App, { target: host, props: { store, copy: async () => {} } });
  await tick();
  return { host, store, backend };
}
const ctl = (h: HTMLElement) => h.querySelector<HTMLElement>('.upd-ctl')!;
const status = (h: HTMLElement) => ctl(h).querySelector('.upd-status')!.textContent;
const btn = (h: HTMLElement) => ctl(h).querySelector<HTMLButtonElement>('button.upd-btn')!;
// Everything that determines the row's box: its children, their classes (minus state classes) and the single button.
const shape = (h: HTMLElement) =>
  `${ctl(h).children.length}:${ctl(h).children[0].className.replace(' upd-err', '')}:${btn(h).className.split(' ').filter((c) => c === 'upd-btn').join()}`;

function spy(backend: MockBackend): string[] {
  const seen: string[] = [];
  const orig = backend.call.bind(backend);
  backend.call = ((m: string, p: unknown) => { seen.push(m); return orig(m, p); }) as typeof backend.call;
  return seen;
}

describe('Settings > Check for updates', () => {
  it('idle: a button with a refresh icon next to the version', async () => {
    const { host } = await settings({});
    expect(btn(host).textContent).toContain('Check for updates');
    expect(btn(host).querySelector('svg')?.getAttribute('data-icon')).toBe('refresh-cw');
    expect(ctl(host).parentElement?.textContent).toContain('v1.0.28');
    expect(status(host)).toBe('');
  });

  it('checking disables the button and a second call is not started', async () => {
    const { host, store, backend } = await settings({ updateCheckDelayMs: 30 });
    const seen = spy(backend);
    const idle = shape(host);
    btn(host).click();
    await tick();
    expect(btn(host).disabled).toBe(true);
    expect(btn(host).textContent).toContain('Checking…');
    expect(status(host)).toBe('Checking…');
    expect(shape(host)).toBe(idle);
    void store.checkForUpdates();
    void store.checkForUpdates();
    await new Promise((r) => setTimeout(r, 80));
    await tick();
    expect(seen.filter((m) => m === 'update.check').length).toBe(1);
    expect(status(host)).toBe('Up to date (v1.0.28)');
    expect(btn(host).disabled).toBe(false);
  });

  const cases: [string, Partial<MockSeed>, string][] = [
    ['up to date', {}, 'Up to date (v1.0.28)'],
    ['update available', { updateCheck: { status: 'available', tag: '1.0.29' } }, 'Update available v1.0.29'],
    ['offline', { updateCheck: { status: 'error', error: 'offline' } }, "Can't reach GitHub"],
    ['rate limited', { updateCheck: { status: 'error', error: 'rate_limited' } }, 'GitHub rate limit reached'],
    ['other error', { updateCheck: { status: 'error', error: 'other' } }, 'Update check failed'],
  ];
  for (const [name, seed, text] of cases) {
    it(`renders ${name} inline with the same row structure and no dialog`, async () => {
      const { host } = await settings(seed);
      const idle = shape(host);
      btn(host).click();
      await tick();
      await tick();
      expect(status(host)).toBe(text);
      expect(host.querySelector('[role="dialog"]')).toBeNull();
      expect(ctl(host).querySelectorAll('button').length).toBe(1);
      expect(shape(host)).toBe(idle);
      expect(host.querySelector('.upd-status.upd-err') !== null).toBe(name !== 'up to date' && name !== 'update available');
    });
  }

  it('update available offers Install, which calls update.apply (the startup path) and shows progress', async () => {
    const { host, store, backend } = await settings({ updateCheck: { status: 'available', tag: '1.0.29' } });
    const seen = spy(backend);
    btn(host).click();
    await tick(); await tick();
    expect(btn(host).textContent).toContain('Install');
    expect(btn(host).querySelector('svg')?.getAttribute('data-icon')).toBe('download');
    btn(host).click();
    await tick();
    expect(seen).toEqual(['update.check', 'update.apply']);
    expect(store.updateCheck.phase).toBe('applying');
    backend.push('update.progress', { pct: 40 });
    await tick();
    expect(status(host)).toBe('Installing v1.0.29… 40%');
    expect(btn(host).disabled).toBe(true);
    expect(host.querySelector('[role="dialog"]')).toBeNull();
    backend.push('update.failed', { message: 'update failed' });
    await tick();
    expect(status(host)).toBe('Update check failed');
    expect(btn(host).textContent).toContain('Check for updates');
  });

  it('a refused stale Install shows a specific message', async () => {
    const { host, backend } = await settings({ updateCheck: { status: 'available', tag: '1.0.29' } });
    btn(host).click();
    await tick(); await tick();
    btn(host).click();
    await tick();
    backend.push('update.failed', { message: 'A newer update was found. Check again.' });
    await tick();
    expect(status(host)).toBe('A newer update was found. Check again.');
    expect(host.querySelector('.upd-status.upd-err')).not.toBeNull();
  });

  it('not licensed: no check row', async () => {
    const seed = defaultSeed();
    seed.boot.mode = 'notLicensed';
    const { host } = await settings({ boot: seed.boot });
    expect(host.querySelector('.upd-ctl')).toBeNull();
  });
});
