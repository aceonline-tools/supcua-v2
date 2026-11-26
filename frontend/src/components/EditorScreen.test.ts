import { beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import EditorScreen from './EditorScreen.svelte';
import css from '../app.css?inline';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;

const count = (m: string) => mock.calls.filter((c) => c.method === m).length;
const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); flushSync(); };
const types = () => store.editor.script!.actions.map((a) => a.type);
const rows = () => Array.from(host.querySelectorAll<HTMLElement>('.action-row'));
const ev = (type: string, x: number, y: number, pointerId = 1) => new PointerEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0, pointerId });
let app: ReturnType<typeof mount>;

beforeEach(async () => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  await store.openEditor('s3');
  app = mount(EditorScreen, { target: host, props: { store } });
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

  it('window blur cancels the drag with no change and no save', async () => {
    const before = types();
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 24 * 3 + 2));
    window.dispatchEvent(new Event('blur'));
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(types()).toEqual(before);
    expect(host.querySelector('.drop-line')).toBeNull();
    await store.editor.close();
    expect(count('editor.save')).toBe(0);
  });

  it('hidden visibility and lost pointer capture cancel the drag', async () => {
    const before = types();
    const grip = host.querySelectorAll<HTMLElement>('.grip')[0];
    grip.dispatchEvent(ev('pointerdown', 5, 12));
    grip.dispatchEvent(new Event('lostpointercapture'));
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(types()).toEqual(before);
    grip.dispatchEvent(ev('pointerdown', 5, 12));
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(types()).toEqual(before);
    vi.restoreAllMocks();
  });

  it('unmounting during a drag removes listeners and later pointerup mutates nothing', async () => {
    const before = types();
    const removed = vi.spyOn(window, 'removeEventListener');
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 24 * 3 + 2));
    await unmount(app);
    for (const t of ['pointermove', 'pointerup', 'pointercancel', 'blur']) expect(removed).toHaveBeenCalledWith(t, expect.any(Function));
    const describes = count('editor.describeActions');
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(types()).toEqual(before);
    expect(count('editor.describeActions')).toBe(describes);
    removed.mockRestore();
  });

  it('closing the editor during a drag cancels it', async () => {
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    await store.editor.close();
    flushSync();
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(count('editor.save')).toBe(0);
  });

  it('ignores move/up from other pointers', async () => {
    const before = types();
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12, 1));
    window.dispatchEvent(ev('pointermove', 50, 24 * 3 + 2, 2));
    flushSync();
    expect(host.querySelector('.drop-line')).toBeNull();
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2, 2));
    await settle();
    expect(types()).toEqual(before);
    window.dispatchEvent(ev('pointerup', 50, 12, 1));
    await settle();
    expect(types()).toEqual(before);
  });

  it('pointerup recomputes the gap from its own coordinates', async () => {
    const before = types();
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 24 * 2 + 2));
    window.dispatchEvent(ev('pointerup', 50, 24 * 5 + 2));
    await settle();
    expect(types()).toEqual([...before.slice(1, 5), before[0], ...before.slice(5)]);
  });

  const caretIdx = () => host.querySelector('.caret-line')?.getAttribute('data-index') ?? null;

  it('caret index follows selection, move, insert, delete and drag-drop', async () => {
    const ed = store.editor;
    expect(caretIdx()).toBe(String(ed.script!.actions.length));
    rows()[2].click();
    flushSync();
    expect(caretIdx()).toBe('3');
    Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Down')!.click();
    await settle();
    expect(caretIdx()).toBe(String(ed.insertAt));
    expect(caretIdx()).toBe('4');
    await ed.addSimple('mouse_left_click');
    await settle();
    expect(caretIdx()).toBe(String(ed.selected + 1));
    await ed.deleteSelected();
    await settle();
    expect(caretIdx()).toBe(String(ed.selected + 1));
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 24 * 3 + 2));
    window.dispatchEvent(ev('pointerup', 50, 24 * 3 + 2));
    await settle();
    expect(caretIdx()).toBe(String(ed.insertAt));
    expect(caretIdx()).toBe('3');
  });

  it('caret is hidden during a drag and restored after cancel', async () => {
    host.querySelectorAll<HTMLElement>('.grip')[0].dispatchEvent(ev('pointerdown', 5, 12));
    window.dispatchEvent(ev('pointermove', 50, 24 * 3 + 2));
    flushSync();
    expect(caretIdx()).toBeNull();
    expect(host.querySelector('.drop-line')).not.toBeNull();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await settle();
    expect(caretIdx()).not.toBeNull();
    expect(host.querySelector('.drop-line')).toBeNull();
  });

  it('caret shows at the top for an empty list', async () => {
    await unmount(app);
    await store.editor.close();
    await store.openNew();
    host.replaceChildren();
    app = mount(EditorScreen, { target: host, props: { store } });
    flushSync();
    expect(caretIdx()).toBe('0');
  });

  it('the stylesheet is loaded and an empty list keeps one row of height so the caret is visible', async () => {
    expect(css).toContain('.drop-line');
    expect(css).toMatch(/\.action-list \.list\.empty\{min-height:var\(--row-h\)\}/);
    await unmount(app);
    await store.editor.close();
    await store.openNew();
    host.replaceChildren();
    app = mount(EditorScreen, { target: host, props: { store } });
    flushSync();
    expect(host.querySelector('.list')!.classList.contains('empty')).toBe(true);
    expect(host.querySelector('.caret-line')).not.toBeNull();
    expect(rows()).toHaveLength(0);
  });

  it('blinks, and is steady under prefers-reduced-motion', () => {
    expect(css).toMatch(/\.caret-line\{[^}]*animation:caret-blink/);
    expect(css).toMatch(/@media \(prefers-reduced-motion:reduce\)\{\.caret-line\{animation:none\}\}/);
  });
});
