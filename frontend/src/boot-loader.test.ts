import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from './api/client';
import { MockBackend } from './api/mock';
import { StrictNotLicensedBackend } from './api/strictMock';
import { trackFirstCalls } from './lib/loader';
import { AppStore } from './lib/store.svelte';
import App from './App.svelte';

// Integration of v1.4.1 (not-licensed early return) with the startup loader's first-call tracking,
// using the real store, the strict not-licensed host and the same method list main.ts tracks.
const TRACKED = ['app.getBootState', 'state.get', 'scripts.list', 'settings.get', 'app.ready'];

let host: HTMLElement;
let cmp: ReturnType<typeof mount> | undefined;
beforeEach(() => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
});
afterEach(() => { if (cmp) unmount(cmp); cmp = undefined; });

describe('loader hand-off on the integrated store', () => {
  it('not licensed: only getBootState and app.ready are called, the loader settles clean, the licence screen renders', async () => {
    const backend = new StrictNotLicensedBackend();
    const { client, loads } = trackFirstCalls(createClient(backend), TRACKED);
    const store = new AppStore(client);
    const init = store.init();
    cmp = mount(App, { target: host, props: { store, copy: async () => {} } });
    await init;
    expect(await loads.settled()).toBeNull();
    expect(loads.notLicensed()).toBe(true);
    flushSync();
    expect(backend.calls.map((c) => c.method).filter((m) => TRACKED.includes(m))).toEqual(['app.getBootState', 'app.ready']);
    expect(store.error).toBe('');
    expect(host.textContent).toContain('Not Licensed');
  });

  it('licensed: every tracked load is made and the hand-off waits for all of them', async () => {
    const backend = new MockBackend();
    const { client, loads } = trackFirstCalls(createClient(backend), TRACKED);
    const store = new AppStore(client);
    await store.init();
    expect(await loads.settled()).toBeNull();
    expect(loads.notLicensed()).toBe(false);
    const called = backend.calls.map((c) => c.method);
    for (const m of ['app.getBootState', 'state.get', 'scripts.list', 'settings.get', 'app.ready']) expect(called, m).toContain(m);
  });
});
