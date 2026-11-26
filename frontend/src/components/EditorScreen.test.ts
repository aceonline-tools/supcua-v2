import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import EditorScreen from './EditorScreen.svelte';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;

const count = (m: string) => mock.calls.filter((c) => c.method === m).length;
const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); flushSync(); };
const types = () => store.editor.script!.actions.map((a) => a.type);
const rows = () => Array.from(host.querySelectorAll<HTMLElement>('.action-row'));
const ev = (type: string, x: number, y: number) => new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0 });

beforeEach(async () => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  await store.openEditor('s3');
  mount(EditorScreen, { target: host, props: { store } });
  flushSync();
  const list = host.querySelector<HTMLElement>('.list')!;
  list.getBoundingClientRect = () => ({ top: 0, bottom: rows().length * 24, left: 0, right: 300, width: 300, height: rows().length * 24, x: 0, y: 0, toJSON() {} });
  rows().forEach((r, i) => { r.getBoundingClientRect = () => ({ top: i * 24, bottom: i * 24 + 24, left: 0, right: 300, width: 300, height: 24, x: 0, y: i * 24, toJSON() {} }); });
});

describe('drag reorder', () => {
  it('a completed drag reorders once through the existing path and persists the new order', async () => {
    const before = types();
    const describes = count('editor.describeActions');
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    flushSync();
    window.dispatchEvent(ev('pointermove', 50, 24 * 3 + 2));
    flushSync();
    expect(host.querySelector('.drop-line')).not.toBeNull();
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(host.querySelector('.drop-line')).toBeNull();
    expect(count('editor.describeActions') - describes).toBe(1);
    const expected = [...before.slice(1, 3), before[0], ...before.slice(3)];
    expect(types()).toEqual(expected);
    expect(store.editor.selected).toBe(2);
    expect(await store.editor.close()).toBe('closed');
    expect(count('editor.save')).toBe(1);
    const saved = mock.calls.find((c) => c.method === 'editor.save')!.params as { script: { actions: { type: string }[] } };
    expect(saved.script.actions.map((a) => a.type)).toEqual(expected);
  });

  it('dropping onto itself changes nothing and does not save', async () => {
    const before = types();
    const describes = count('editor.describeActions');
    host.querySelectorAll<HTMLElement>('.grip')[1].dispatchEvent(ev('pointerdown', 5, 30));
    window.dispatchEvent(ev('pointermove', 50, 36));
    flushSync();
    expect(host.querySelector('.drop-line')).toBeNull();
    window.dispatchEvent(ev('pointerup', 50, 36));
    await settle();
    expect(types()).toEqual(before);
    expect(count('editor.describeActions')).toBe(describes);
    await store.editor.close();
    expect(count('editor.save')).toBe(0);
  });

  it('Esc cancels the drag', async () => {
    const before = types();
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 70));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    window.dispatchEvent(ev('pointerup', 50, 70));
    await settle();
    expect(types()).toEqual(before);
    expect(host.querySelector('.drop-line')).toBeNull();
    await store.editor.close();
    expect(count('editor.save')).toBe(0);
  });

  it('dropping outside the list cancels', async () => {
    const before = types();
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 70));
    window.dispatchEvent(ev('pointerup', 900, 70));
    await settle();
    expect(types()).toEqual(before);
    await store.editor.close();
    expect(count('editor.save')).toBe(0);
  });

  it('Up/Down buttons still work', async () => {
    const before = types();
    rows()[1].click();
    flushSync();
    Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Up')!.click();
    await settle();
    expect(types().slice(0, 2)).toEqual([before[1], before[0]]);
  });
});
