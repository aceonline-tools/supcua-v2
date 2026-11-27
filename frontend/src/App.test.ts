import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from './api/client';
import { MockBackend, defaultSeed } from './api/mock';
import { NOT_LICENSED_ALLOWED, StrictNotLicensedBackend } from './api/strictMock';
import { reportMounted, reportRendered } from './lib/diag';
import { AppStore } from './lib/store.svelte';
import App from './App.svelte';

let host: HTMLElement;
let cmp: ReturnType<typeof mount> | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  host = document.createElement('div');
  document.body.replaceChildren(host);
});

afterEach(() => {
  if (cmp) unmount(cmp);
  cmp = undefined;
  vi.useRealTimers();
});

describe('not-licensed boot', () => {
  it('shows the licence screen, no error, and calls only allow-listed methods', async () => {
    const seed = defaultSeed();
    seed.boot.macs = ['02:00:00:00:00:AA', '02:00:00:00:00:BB'];
    const backend = new StrictNotLicensedBackend(seed);
    const client = createClient(backend);
    const store = new AppStore(client);
    const copied: string[] = [];
    const copy = async (text: string) => { await client.call('clipboard.write', { text }); copied.push(text); };

    const init = store.init();
    cmp = mount(App, { target: host, props: { store, copy } });
    reportMounted(client);
    reportRendered(client);
    await vi.advanceTimersByTimeAsync(60_000);
    await init;
    flushSync();

    expect(host.textContent).toContain('Not Licensed');
    expect(host.querySelector<HTMLInputElement>('input[aria-label="Machine address"]')!.value).toBe('02:00:00:00:00:AA');
    expect(store.error).toBe('');
    expect(store.loaded).toBe(true);

    host.querySelector<HTMLButtonElement>('button')!.click();
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
    expect(copied).toEqual(['02:00:00:00:00:AA']);
    expect(host.querySelector('button')!.textContent).toBe('Copied');

    const methods = backend.calls.map((c) => c.method);
    expect(methods[0]).toBe('app.getBootState');
    expect(methods).toContain('app.ready');
    expect(methods.filter((m) => !NOT_LICENSED_ALLOWED.includes(m))).toEqual([]);
  });
});

describe('licensed boot', () => {
  it('keeps the startup order: boot, state, scripts/settings, ready', async () => {
    vi.useRealTimers();
    const backend = new MockBackend();
    const store = new AppStore(createClient(backend));
    await store.init();
    const methods = backend.calls.map((c) => c.method);
    expect(methods[0]).toBe('app.getBootState');
    expect(methods.indexOf('state.get')).toBeGreaterThan(-1);
    expect(methods.indexOf('scripts.list')).toBeGreaterThan(methods.indexOf('state.get'));
    expect(methods.at(-1)).toBe('app.ready');
    expect(store.error).toBe('');
  });
});
