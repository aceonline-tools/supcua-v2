import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import EditorScreen from './EditorScreen.svelte';
import type { KeyPressAction } from '../lib/keySequence';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); flushSync(); };
const q = <T extends HTMLElement>(sel: string) => host.querySelector<T>(sel)!;
const chips = () => Array.from(host.querySelectorAll('.chip kbd')).map((e) => e.textContent);
const click = async (el: HTMLElement) => { el.click(); await settle(); };
const keyAt = (i: number) => store.editor.script!.actions[i] as KeyPressAction;
const cid = () => (mock as unknown as { activeCap: { id: number } }).activeCap.id;

async function openDialog(action: KeyPressAction | null) {
  console.log('DBG', JSON.stringify(store.editor.script!.actions.map((a) => a.type)), store.editor.catalog.length);
  if (action) store.editor.script!.actions[1] = action as never;
  store.editor.selected = 1;
  store.editor.editSelected();
  await settle();
  console.log('DBG2', !!store.editor.actionDialog, host.innerHTML.length);
}
const ok = () => Array.from(host.querySelectorAll<HTMLButtonElement>('.dialog-footer button')).find((b) => b.textContent!.includes('OK'))!;
const type = async (text: string) => {
  const input = q<HTMLInputElement>('input[aria-label="Add key"]');
  input.value = text;
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await click(q('button[aria-label="Add typed key"]'));
};

beforeEach(async () => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  await store.openEditor('s3');
  mount(EditorScreen, { target: host, props: { store } });
  await settle();
});

describe('key sequence dialog', () => {
  it('a legacy single key shows one chip and saves back as legacy {type,key}', async () => {
    await openDialog(null);
    expect(chips()).toEqual(['1']);
    await click(ok());
    expect(keyAt(1)).toEqual({ type: 'key_press', key: '1' });
  });

  it('loads a multi-key action as chips and saves the exact contract payload', async () => {
    await openDialog({ type: 'key_press', keys: [{ key: 'a', mods: ['ctrl', 'shift'] }, { key: 'b' }], key_delay_ms: 80 });
    expect(chips()).toEqual(['Ctrl+Shift+A', 'B']);
    expect(q('.seq-preview').textContent).toContain('Ctrl+Shift+A → B');
    expect(q<HTMLInputElement>('#ap-key-delay').value).toBe('80');
    await click(ok());
    expect(keyAt(1)).toEqual({ type: 'key_press', keys: [{ key: 'a', mods: ['ctrl', 'shift'] }, { key: 'b' }], key_delay_ms: 80 });
  });

  it('adds by typing, removes and reorders', async () => {
    await openDialog(null);
    await type('Alt+F4');
    await type('Enter');
    expect(chips()).toEqual(['1', 'Alt+F4', 'Enter']);
    await click(q('button[aria-label="Move Enter up"]'));
    expect(chips()).toEqual(['1', 'Enter', 'Alt+F4']);
    await click(q('button[aria-label="Remove 1"]'));
    expect(chips()).toEqual(['Enter', 'Alt+F4']);
    await click(ok());
    expect(keyAt(1)).toEqual({ type: 'key_press', keys: [{ key: 'enter' }, { key: 'f4', mods: ['alt'] }], key_delay_ms: 50 });
  });

  it('flags an unknown typed key and adds nothing', async () => {
    await openDialog(null);
    await type('nosuchkey');
    expect(chips()).toEqual(['1']);
    expect(host.textContent).toContain('Unknown key');
  });

  it('blocks an empty list and a bad delay inline', async () => {
    await openDialog(null);
    await click(q('button[aria-label="Remove 1"]'));
    expect(host.textContent).toContain('Add at least one key.');
    expect(ok().disabled).toBe(true);
    await type('a');
    expect(ok().disabled).toBe(false);
    const d = q<HTMLInputElement>('#ap-key-delay');
    for (const v of ['5001', '-1', '', '1.5']) {
      d.value = v;
      d.dispatchEvent(new Event('input', { bubbles: true }));
      await settle();
      expect(ok().disabled, v).toBe(true);
      expect(host.textContent).toContain('Delay must be');
    }
    d.value = '0';
    d.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    expect(ok().disabled).toBe(false);
  });

  it('caps at 32 keys', async () => {
    await openDialog({ type: 'key_press', keys: Array.from({ length: 32 }, () => ({ key: 'a' })), key_delay_ms: 50 });
    expect(chips()).toHaveLength(32);
    expect(q<HTMLButtonElement>('button[aria-label="Add typed key"]').disabled).toBe(true);
    expect(q<HTMLButtonElement>('button[aria-label="Record keys"]').disabled).toBe(true);
  });

  it('records keys in order with modifiers, and Esc stops without recording it', async () => {
    await openDialog(null);
    await click(q('button[aria-label="Record keys"]'));
    await settle();
    expect(store.editor.capturing).toBe('keys');
    expect(mock.calls.filter((c) => c.method === 'capture.key.start')).toHaveLength(1);
    for (const key of ['ctrl', 'x', 'y']) { mock.push('capture.key', { capture_id: cid(), key }); await settle(); }
    expect(chips()).toEqual(['Ctrl+X', 'Y']);
    expect(store.editor.capturing).toBe('keys');
    mock.push('capture.key', { capture_id: cid(), key: 'escape' });
    await settle();
    expect(store.editor.capturing).toBeNull();
    expect(chips()).toEqual(['Ctrl+X', 'Y']);
    expect(mock.calls.some((c) => c.method === 'capture.stop')).toBe(true);
  });

  it('the Stop button ends recording', async () => {
    await openDialog(null);
    await click(q('button[aria-label="Record keys"]'));
    await click(q('button[aria-label="Stop recording"]'));
    expect(store.editor.capturing).toBeNull();
  });
});
