import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import type { AppStore } from '../lib/store.svelte';
import { decisions, editorStore } from '../lib/test-editor';
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
  if (action) store.editor.script!.actions[3] = action as never;
  store.editor.selected = 3;
  store.editor.editSelected();
  await settle();
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
  store = await editorStore(mock, 's3');
  mount(EditorScreen, { target: host, props: { store } });
  await settle();
});

describe('key sequence dialog', () => {
  it('a legacy single key shows one chip and saves back as legacy {type,key}', async () => {
    await openDialog(null);
    expect(chips()).toEqual(['1']);
    await click(ok());
    expect(keyAt(3)).toEqual({ type: 'key_press', key: '1' });
  });

  it('loads a multi-key action as chips and saves the exact contract payload', async () => {
    await openDialog({ type: 'key_press', keys: [{ key: 'a', mods: ['ctrl', 'shift'] }, { key: 'b' }], key_delay_ms: 80 });
    expect(chips()).toEqual(['Ctrl+Shift+A', 'B']);
    expect(q('.seq-preview').textContent).toContain('Ctrl+Shift+A → B');
    expect(q<HTMLInputElement>('#ap-key-delay').value).toBe('80');
    await click(ok());
    expect(keyAt(3)).toEqual({ type: 'key_press', keys: [{ key: 'a', mods: ['ctrl', 'shift'] }, { key: 'b' }], key_delay_ms: 80 });
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
    expect(keyAt(3)).toEqual({ type: 'key_press', keys: [{ key: 'enter' }, { key: 'f4', mods: ['alt'] }], key_delay_ms: 50 });
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
    expect(chips()).toEqual(['1', 'Ctrl+X', 'Y']);
    expect(store.editor.capturing).toBe('keys');
    mock.push('capture.key', { capture_id: cid(), key: 'escape' });
    await settle();
    expect(store.editor.capturing).toBeNull();
    expect(chips()).toEqual(['1', 'Ctrl+X', 'Y']);
    expect(mock.calls.some((c) => c.method === 'capture.stop')).toBe(true);
  });

  it('records modifiers from the host flags: held across keys, released between', async () => {
    await openDialog(null);
    await click(q('button[aria-label="Record keys"]'));
    mock.pushKey('ctrl', ['ctrl']); await settle();
    mock.pushKey('x', ['ctrl']); await settle();
    mock.pushKey('y', ['ctrl']); await settle();
    mock.pushKey('z', []); await settle();
    expect(chips()).toEqual(['1', 'Ctrl+X', 'Ctrl+Y', 'Z']);
  });

  it('the Stop button ends recording', async () => {
    await openDialog(null);
    await click(q('button[aria-label="Record keys"]'));
    await click(q('button[aria-label="Stop recording"]'));
    expect(store.editor.capturing).toBeNull();
  });
});

describe('existing capture behaviour is unchanged', () => {
  it('single-key capture ignores the new mods field and still ends after one key', async () => {
    await store.editor.startKeyCapture();
    await settle();
    mock.pushKey('q', ['ctrl']);
    await settle();
    expect(store.editor.builder.key).toBe('q');
    expect(store.editor.capturing).toBeNull();
  });

  it('hotkey capture (HotkeyInput) still sets the hotkey and ends', async () => {
    await store.editor.startCapture('toggle');
    await settle();
    mock.push('capture.hotkey', { capture_id: mock.currentCaptureId, field: 'toggle', hotkey: { modifiers: ['ctrl'], key: 'f9' }, display: 'Ctrl + F9' });
    await settle();
    expect(store.editor.script!.toggle_hotkey).toEqual({ modifiers: ['ctrl'], key: 'f9' });
    expect(store.editor.capturing).toBeNull();
  });
});

describe('recording lifecycle and focus', () => {
  const calls = (m: string) => mock.calls.filter((c) => c.method === m).length;

  it('a start queued behind the dialog closing never leaves a live capture', async () => {
    await openDialog(null);
    void store.editor.startKeyRecording();
    store.editor.closeActionDialog();
    await settle();
    await settle();
    expect(store.editor.capturing).toBeNull();
    expect(calls('capture.key.start')).toBe(calls('capture.stop'));
  });

  it('submitting while a start is queued also cancels it', async () => {
    await openDialog(null);
    void store.editor.startKeyRecording();
    await click(ok());
    await settle();
    expect(store.editor.capturing).toBeNull();
    expect(calls('capture.key.start')).toBe(calls('capture.stop'));
  });

  it('Esc ends a recording even when no dialog is open', async () => {
    await openDialog(null);
    await click(q('button[aria-label="Record keys"]'));
    expect(store.editor.capturing).toBe('keys');
    store.editor.actionDialog = null;
    mock.pushKey('x', []);
    await settle();
    expect(store.editor.capturing).toBe('keys');
    mock.pushKey('escape', []);
    await settle();
    expect(store.editor.capturing).toBeNull();
    expect(calls('capture.stop')).toBe(1);
  });

  it('the sequence dialog focuses its first control', async () => {
    await openDialog(null);
    expect(document.activeElement).toBe(q('input[aria-label="Add key"]'));
  });
});
