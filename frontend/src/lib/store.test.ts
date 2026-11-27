import { beforeEach, describe, expect, it } from 'vitest';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from './store.svelte';

let mock: MockBackend;
let store: AppStore;

const count = (m: string) => mock.calls.filter((c) => c.method === m).length;

beforeEach(async () => {
  document.documentElement.removeAttribute('data-theme');
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
});

describe('theme', () => {
  it('switches, applies to the document and persists through the backend', async () => {
    expect(document.documentElement.dataset.theme).toBe('dark');
    await store.setTheme('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    const fresh = new AppStore(createClient(mock));
    await fresh.init();
    expect(fresh.themeSetting).toBe('light');
    expect((await createClient(mock).call('settings.get')).theme).toBe('light');
  });

  it('system follows prefers-color-scheme', async () => {
    store.systemDark = false;
    await store.setTheme('system');
    expect(document.documentElement.dataset.theme).toBe('light');
    store.systemDark = true;
    store.applyTheme();
    expect(document.documentElement.dataset.theme).toBe('dark');
  });
});

describe('groups', () => {
  it('collapse hides children, persists, and leaves enabled state untouched', async () => {
    const before = store.scripts.map((s) => s.enabled);
    const visible = store.rows.length;
    await store.toggleCollapsed('Farming');
    expect(store.rows.length).toBe(visible - 3);
    expect(store.rows.some((r) => r.kind === 'script' && r.script.group === 'Farming')).toBe(false);
    expect(store.scripts.map((s) => s.enabled)).toEqual(before);
    expect(count('scripts.setEnabled') + count('scripts.setGroupEnabled')).toBe(0);
    expect((await createClient(mock).call('settings.get')).collapsed_groups).toEqual(['Farming']);
    await store.loadScripts();
    expect(store.collapsed).toEqual(['Farming']);
  });

  it('group checkbox toggles all children in one call', async () => {
    await store.setGroupEnabled('Farming', true);
    expect(count('scripts.setGroupEnabled')).toBe(1);
    expect(count('scripts.setEnabled')).toBe(0);
    expect(store.scripts.filter((s) => s.group === 'Farming').every((s) => s.enabled)).toBe(true);
    const row = store.rows.find((r) => r.kind === 'group' && r.group.name === 'Farming');
    expect(row && row.kind === 'group' && row.checked).toBe(true);
  });
});

describe('script actions', () => {
  it('clone calls scripts.clone and selects the copy', async () => {
    store.select('s3');
    await store.cloneSelected();
    expect(count('scripts.clone')).toBe(1);
    expect(mock.calls.find((c) => c.method === 'scripts.clone')?.params).toEqual({ id: 's3' });
    expect(store.scripts).toHaveLength(6);
    expect(store.selected?.name).toBe('Auto attack (copy)');
  });

  it('delete needs confirmation', async () => {
    store.select('s5');
    store.requestDelete();
    expect(count('scripts.delete')).toBe(0);
    await store.confirmDelete();
    expect(count('scripts.delete')).toBe(1);
    expect(store.scripts.some((s) => s.id === 's5')).toBe(false);
    expect(store.selectedId).toBeNull();
  });

  it('flags a script unsaved on save.failed and clears it after retry', async () => {
    mock.push('save.failed', { scriptId: 's1', name: 'Defense buff', message: 'disk' });
    expect(store.unsaved['s1']).toBe('disk');
    await store.retrySave('s1');
    expect(store.unsaved['s1']).toBeUndefined();
  });
});

describe('settings', () => {
  it('reports restart-only fields and rejects invalid values', async () => {
    expect((await store.updateSettings({ key_press_duration_ms: 20 })).ok).toBe(true);
    expect(store.restartPending).toContain('key_press_duration_ms');
    const bad = await store.updateSettings({ key_press_duration_ms: 0 });
    expect(bad).toEqual({ ok: false, field: 'key_press_duration_ms', code: 'INVALID_SETTING' });
    expect(store.settings?.key_press_duration_ms).toBe(20);
  });
});

describe('theme persistence for first paint', () => {
  it('stores the selected theme (including system) for the pre-paint script', async () => {
    await store.setTheme('light');
    expect(localStorage.getItem('supcua.theme')).toBe('light');
    await store.setTheme('system');
    expect(localStorage.getItem('supcua.theme')).toBe('system');
  });
});
