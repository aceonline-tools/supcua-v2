import { afterAll, afterEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend, seedFromQuery } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import App from '../App.svelte';
import Toggle from './Toggle.svelte';

let host: HTMLElement;
let app: ReturnType<typeof mount> | undefined;
let store: AppStore;
let total = 0;
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); flushSync(); };

async function boot(search = '') {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  store = new AppStore(createClient(new MockBackend(seedFromQuery(search))));
  await store.init();
  app = mount(App, { target: host, props: { store, copy: async () => {} } });
  await settle();
}

function expectIcons(label: string, min: number) {
  const els = Array.from(host.querySelectorAll<HTMLElement>('button, [role="button"]'));
  expect(els.length, `${label}: buttons found`).toBeGreaterThanOrEqual(min);
  const bare = els.filter((e) => !e.querySelector('svg')).map((e) => (e.getAttribute('aria-label') || e.textContent || e.className).trim().slice(0, 40));
  expect(bare, `${label}: buttons without an svg`).toEqual([]);
  total += els.length;
}

afterAll(() => { expect(total).toBeGreaterThan(100); });
afterEach(() => { if (app) unmount(app); app = undefined; });

describe('rendered buttons all contain an svg icon', () => {
  it('scripts, expanded sidebar, selected row, unsaved retry, error banner', async () => {
    await boot();
    store.select('s3'); store.unsaved['s3'] = 'disk full'; store.error = 'boom';
    await settle();
    expectIcons('scripts expanded', 8);
  });

  it('scripts with the sidebar collapsed', async () => {
    await boot();
    store.setSidebarCollapsed(true);
    await settle();
    expectIcons('scripts collapsed', 4);
  });

  it('delete dialog', async () => {
    await boot();
    store.select('s3'); store.requestDelete();
    await settle();
    expect(host.querySelector('[role="alertdialog"]')).not.toBeNull();
    expectIcons('delete dialog', 8);
  });

  it('update dialog', async () => {
    await boot('?mock=update');
    await settle();
    store.update = { tag: 'v9.9.9', applying: false, pct: 0, failed: '' };
    await settle();
    expect(host.textContent).toContain('Update');
    expectIcons('update dialog', 6);
  });

  it('licence screen', async () => {
    await boot('?mock=license');
    expect(host.querySelector('.license')).not.toBeNull();
    expectIcons('licence', 1);
  });

  it('editor, action dialog, raw JSON, discard and conflict dialogs', async () => {
    await boot();
    await store.openEditor('s3');
    await settle();
    const ed = store.editor;
    expectIcons('editor', 25);
    for (let i = 0; i < ed.script!.actions.length && !ed.actionDialog; i++) { ed.selectAction(i); ed.editSelected(); }
    await settle();
    expect(host.querySelector('#ad-title')).not.toBeNull();
    expectIcons('action dialog', 25);
    ed.closeActionDialog();
    ed.openRaw();
    await settle();
    expect(host.querySelector('#raw-title')).not.toBeNull();
    expectIcons('raw dialog', 25);
    ed.closeRaw();
    ed.discardPrompt = 'Saving the script failed.';
    await settle();
    expect(host.querySelector('#dc-title')).not.toBeNull();
    expectIcons('discard dialog', 25);
    ed.discardPrompt = null;
    ed.conflicts = [{ scriptId: 'x', name: 'Other', field: 'toggle_hotkey', hotkey: { modifiers: [], key: 'F1' } }];
    await settle();
    expect(host.querySelector('#hc-title')).not.toBeNull();
    expectIcons('conflict dialog', 25);
  });

  it('toggle switch carries a check when on and an x when off', async () => {
    host = document.createElement('div');
    document.body.replaceChildren(host);
    const t = mount(Toggle, { target: host, props: { checked: true, label: 'L', onchange: () => {} } });
    flushSync();
    expect(host.querySelector('button')!.getAttribute('role')).toBe('switch');
    expect(host.querySelectorAll('svg path').length).toBe(1);
    expectIcons('toggle on', 1);
    await unmount(t);
  });

});
