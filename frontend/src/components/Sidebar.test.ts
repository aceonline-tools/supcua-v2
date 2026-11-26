import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import Sidebar from './Sidebar.svelte';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;
let cmp: ReturnType<typeof mount> | undefined;

const render = () => {
  if (cmp) unmount(cmp);
  host.innerHTML = '';
  cmp = mount(Sidebar, { target: host, props: { store } });
  flushSync();
};
const toggle = () => host.querySelector<HTMLButtonElement>('.side-toggle')!;
const nav = (label: string) => host.querySelector<HTMLButtonElement>(`button.nav[aria-label="${label}"]`)!;

beforeEach(async () => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  render();
});

describe('sidebar', () => {
  it('starts expanded with brand, labels and the global toggle info', () => {
    expect(toggle().getAttribute('aria-expanded')).toBe('true');
    expect(toggle().getAttribute('aria-label')).toBe('Collapse sidebar');
    expect(host.querySelector('.brand')).not.toBeNull();
    expect(host.textContent).toContain('Global toggle');
    expect(host.textContent).toContain('Settings');
  });

  it('collapses to the icon strip, persists through the backend and restores on a fresh store', async () => {
    toggle().click();
    await Promise.resolve();
    flushSync();
    expect(store.sidebarCollapsed).toBe(true);
    expect(host.querySelector('.sidebar')!.classList.contains('collapsed')).toBe(true);
    expect(toggle().getAttribute('aria-expanded')).toBe('false');
    expect(toggle().getAttribute('aria-label')).toBe('Expand sidebar');
    expect(host.querySelector('.brand')).toBeNull();
    expect(host.querySelector('footer')).toBeNull();
    expect((await createClient(mock).call('settings.get')).sidebar_collapsed).toBe(true);

    store = new AppStore(createClient(mock));
    await store.init();
    render();
    expect(host.querySelector('.sidebar')!.classList.contains('collapsed')).toBe(true);

    toggle().click();
    await Promise.resolve();
    flushSync();
    expect(host.querySelector('footer')!.textContent).toContain('Global toggle');
    expect((await createClient(mock).call('settings.get')).sidebar_collapsed).toBe(false);
  });

  it('switches screens while collapsed', async () => {
    await store.setSidebarCollapsed(true);
    flushSync();
    nav('Settings').click();
    await Promise.resolve();
    flushSync();
    expect(store.route.name).toBe('settings');
    expect(nav('Settings').getAttribute('aria-current')).toBe('page');
    nav('Scripts').click();
    await Promise.resolve();
    flushSync();
    expect(store.route.name).toBe('scripts');
    expect(store.sidebarCollapsed).toBe(true);
  });

  it('rolls back and reports when the backend rejects the change', async () => {
    const real = mock.call.bind(mock);
    mock.call = async (method: string, params?: unknown) => {
      if (method === 'settings.setSidebarCollapsed') throw new Error('save failed');
      return real(method, params);
    };
    await store.setSidebarCollapsed(true);
    expect(store.sidebarCollapsed).toBe(false);
    expect(store.error).not.toBe('');
  });
});

describe('sidebar toggle races', () => {
  const failing = (fail: (n: number) => boolean) => {
    const real = mock.call.bind(mock);
    let n = 0;
    const sent: boolean[] = [];
    mock.call = async (method: string, params?: unknown) => {
      if (method === 'settings.setSidebarCollapsed') {
        sent.push((params as { collapsed: boolean }).collapsed);
        if (fail(++n)) throw new Error('save failed');
      }
      return real(method, params);
    };
    return sent;
  };

  it('two rapid toggles that both fail roll back to the persisted value', async () => {
    failing(() => true);
    const a = store.setSidebarCollapsed(true);
    const b = store.setSidebarCollapsed(false);
    expect(store.sidebarCollapsed).toBe(false);
    await Promise.all([a, b]);
    expect(store.sidebarCollapsed).toBe(false);
    expect((await createClient(mock).call('settings.get')).sidebar_collapsed).toBe(false);
    const c = store.setSidebarCollapsed(true);
    await c;
    expect(store.sidebarCollapsed).toBe(false);
  });

  it('first fails and second succeeds: the second value stays and is persisted', async () => {
    const sent = failing((n) => n === 1);
    const a = store.setSidebarCollapsed(true);
    const b = store.setSidebarCollapsed(false);
    const c = store.setSidebarCollapsed(true);
    await Promise.all([a, b, c]);
    expect(sent).toEqual([true, false, true]);
    expect(store.sidebarCollapsed).toBe(true);
    expect((await createClient(mock).call('settings.get')).sidebar_collapsed).toBe(true);
  });

  it('a failing last toggle rolls back to the last confirmed value, not an unpersisted one', async () => {
    failing((n) => n === 2);
    const a = store.setSidebarCollapsed(true);
    const b = store.setSidebarCollapsed(false);
    await Promise.all([a, b]);
    expect(store.sidebarCollapsed).toBe(true);
    expect((await createClient(mock).call('settings.get')).sidebar_collapsed).toBe(true);
  });

  it('loadSettings during a pending toggle does not clobber the optimistic value', async () => {
    const real = mock.call.bind(mock);
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    mock.call = async (method: string, params?: unknown) => {
      if (method === 'settings.setSidebarCollapsed') await gate;
      return real(method, params);
    };
    const toggling = store.setSidebarCollapsed(true);
    await store.loadSettings();
    expect(store.sidebarCollapsed).toBe(true);
    release();
    await toggling;
    await store.loadSettings();
    expect(store.sidebarCollapsed).toBe(true);
  });

  it('a loadSettings issued before a toggle and answered after it does not overwrite it', async () => {
    const real = mock.call.bind(mock);
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    mock.call = async (method: string, params?: unknown) => {
      if (method !== 'settings.get') return real(method, params);
      const stale = await real(method, params);
      await gate;
      return stale;
    };
    const loading = store.loadSettings();
    await store.setSidebarCollapsed(true);
    release();
    await loading;
    expect(store.sidebarCollapsed).toBe(true);
  });
});
