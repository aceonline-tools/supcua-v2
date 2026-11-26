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
