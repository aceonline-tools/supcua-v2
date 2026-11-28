import { afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import App from '../App.svelte';

let app: ReturnType<typeof mount> | undefined;
afterEach(() => { if (app) unmount(app); app = undefined; });

async function settings(collapsed = false) {
  const host = document.createElement('div');
  document.body.replaceChildren(host);
  const store = new AppStore(createClient(new MockBackend()));
  await store.init();
  store.setSidebarCollapsed(collapsed);
  store.go({ name: 'settings' });
  app = mount(App, { target: host, props: { store, copy: async () => {} } });
  for (let i = 0; i < 8; i++) await Promise.resolve();
  flushSync();
  return host;
}

function buttons(host: HTMLElement) {
  const els = Array.from(host.querySelectorAll<HTMLElement>('button, [role="button"]'));
  const bare = els.filter((e) => !e.querySelector('svg')).map((e) => (e.getAttribute('aria-label') || e.textContent || e.className).trim().slice(0, 40));
  return { els, bare };
}

describe('Settings buttons all contain an svg icon', () => {
  it('settings with the Overlay section, defaults and the reset button', async () => {
    const host = await settings();
    expect(host.querySelector('[data-testid="overlay-style"]')).not.toBeNull();
    const reset = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('Reset to defaults'));
    expect(reset?.querySelector('svg')?.getAttribute('data-icon')).toBe('rotate-ccw');
    const { els, bare } = buttons(host);
    expect(els.length).toBe(15);
    expect(bare).toEqual([]);
    const themes = Array.from(host.querySelectorAll('[role="radio"]')).map((b) => b.querySelector('svg')?.getAttribute('data-icon'));
    expect(themes).toEqual(['moon', 'sun', 'monitor']);
    expect(host.querySelectorAll('[role="switch"] svg').length).toBeGreaterThanOrEqual(3);
  });

  it('settings with the sidebar collapsed', async () => {
    const { els, bare } = buttons(await settings(true));
    expect(els.length).toBe(15);
    expect(bare).toEqual([]);
  });
});
