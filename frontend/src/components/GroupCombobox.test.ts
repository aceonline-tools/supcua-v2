import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import type { AppStore } from '../lib/store.svelte';
import { decisions, editorStore } from '../lib/test-editor';
import EditorScreen from './EditorScreen.svelte';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;

const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); flushSync(); };
const input = () => host.querySelector<HTMLInputElement>('#ed-group')!;
const opts = () => Array.from(host.querySelectorAll('.combo-list [role=option]'), (o) => o.textContent!);
const group = () => store.editor.script!.group;
const type = (v: string) => { input().focus(); input().value = v; input().dispatchEvent(new Event('input', { bubbles: true })); flushSync(); };
const key = (k: string, init: KeyboardEventInit = {}) => { input().dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init })); flushSync(); };

beforeEach(async () => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = await editorStore(mock, 's3');
  mount(EditorScreen, { target: host, props: { store } });
  flushSync();
});

describe('group combobox', () => {
  it('exposes the ARIA combobox pattern', () => {
    expect(input().getAttribute('role')).toBe('combobox');
    expect(input().getAttribute('aria-expanded')).toBe('false');
    type('b');
    expect(input().getAttribute('aria-expanded')).toBe('true');
    const list = host.querySelector('.combo-list')!;
    expect(input().getAttribute('aria-controls')).toBe(list.id);
    expect(input().getAttribute('aria-activedescendant')).toBe(host.querySelector('.combo-list [role=option][aria-selected=true]')!.id);
  });

  it('filters case-insensitively by substring', () => {
    type('FARM');
    expect(opts()).toEqual(['Farming', 'Create "FARM"']);
    type('uff');
    expect(opts()).toEqual(['Buffs', 'Create "uff"']);
  });

  it('picks an existing name by click', () => {
    type('buf');
    host.querySelector<HTMLElement>('.combo-list [role=option]')!.click();
    flushSync();
    expect(group()).toBe('Buffs');
    expect(input().value).toBe('Buffs');
    expect(host.querySelector('.combo-list')).toBeNull();
  });

  it('picks by keyboard, clamping at both ends', () => {
    type('');
    key('ArrowDown');
    const all = opts();
    expect(all.length).toBeGreaterThanOrEqual(2);
    const sel = () => host.querySelector('.combo-list [aria-selected=true]')!.textContent;
    expect(sel()).toBe(all[0]);
    key('ArrowUp');
    expect(sel()).toBe(all[0]);
    for (let i = 0; i < 20; i++) key('ArrowDown');
    expect(sel()).toBe(all[all.length - 1]);
    key('ArrowUp');
    expect(sel()).toBe(all[all.length - 2]);
    key('Enter');
    expect(group()).toBe(all[all.length - 2]);
    expect(host.querySelector('.combo-list')).toBeNull();
  });

  it('creates a new name through the Create option', () => {
    type('  Raiders ');
    expect(opts()).toEqual(['Create "Raiders"']);
    key('Enter');
    expect(group()).toBe('Raiders');
    expect(store.editor.dirty).toBe(true);
  });

  it('ignores keys during IME composition and works again after compositionend', () => {
    type('Raid');
    key('Enter', { isComposing: true });
    key('ArrowDown', { keyCode: 229 });
    key('Escape', { isComposing: true });
    expect(group()).not.toBe('Raid');
    expect(host.querySelector('.combo-list')).not.toBeNull();
    input().dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
    key('Enter');
    expect(group()).not.toBe('Raid');
    input().dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }));
    key('Enter');
    expect(group()).toBe('Raid');
  });

  it('reopening with ArrowDown activates the first option', () => {
    type('');
    key('ArrowDown');
    key('Escape');
    key('ArrowDown');
    expect(host.querySelector('.combo-list [aria-selected=true]')!.textContent).toBe(opts()[0]);
  });

  it('typing "  buffs " selects the existing "Buffs" and offers no Create', () => {
    type('  buffs ');
    expect(opts()).toEqual(['Buffs']);
    key('Enter');
    expect(group()).toBe('Buffs');
  });

  it('Esc closes without changing the value', () => {
    const before = group();
    type('zzz');
    key('Escape');
    expect(host.querySelector('.combo-list')).toBeNull();
    expect(group()).toBe(before);
    expect(input().value).toBe(before ?? '');
  });

  it('blur keeps the typed text, normalized', () => {
    type(' buffs ');
    input().dispatchEvent(new FocusEvent('blur'));
    flushSync();
    expect(group()).toBe('Buffs');
    expect(host.querySelector('.combo-list')).toBeNull();
  });

  it('empty value clears the group, as today', () => {
    type('   ');
    input().dispatchEvent(new FocusEvent('blur'));
    flushSync();
    expect(group()).toBe('');
  });

  it('saves the new group and the Scripts list shows it', async () => {
    type('Raiders');
    key('Enter');
    await store.editor.guard();
    expect(decisions(mock).at(-1)).toBe('saved');
    const saved = mock.calls.find((c) => c.method === 'editor.save')!.params as { script: { group: string } };
    expect(saved.script.group).toBe('Raiders');
    await store.loadScripts();
    expect(store.rows.some((r) => r.kind === 'group' && r.group.name === 'Raiders')).toBe(true);
    expect(store.scripts.find((s) => s.id === 's3')!.group).toBe('Raiders');
    await settle();
  });
});
