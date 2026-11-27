import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import Settings from './Settings.svelte';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;
let cmp: ReturnType<typeof mount> | undefined;

const settle = async () => { for (let i = 0; i < 8; i++) { await new Promise((r) => setTimeout(r, 0)); flushSync(); } };
const row = (label: string) => host.querySelector<HTMLElement>(`[aria-label="${label}"]`)!.closest('.field-wrap') as HTMLElement;
const btn = (label: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="Capture ${label}"]`)!;
const value = (label: string) => row(label).querySelector('.hk-rec-value')!.textContent!.replace(/\s+/g, ' ').trim();
const key = (type: 'keydown' | 'keyup', code: string, k = code, init: KeyboardEventInit = {}) =>
  window.dispatchEvent(new KeyboardEvent(type, { code, key: k, bubbles: true, cancelable: true, ...init }));
const tap = (code: string) => { key('keydown', code); key('keyup', code); };

beforeEach(async () => {
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  await store.loadSettings();
  host = document.createElement('div');
  document.body.append(host);
  cmp = mount(Settings, { target: host, props: { store } });
  flushSync();
});
afterEach(() => { if (cmp) unmount(cmp); host.remove(); cmp = undefined; });

const start = async (label: string) => { btn(label).click(); await settle(); };

describe('Settings hotkey recorder', () => {
  it('idle row shows the current hotkey and a Capture button with an icon', () => {
    expect(value('Global hotkey')).toBe('RCtrl');
    expect(value('Overlay hotkey')).toBe('Ctrl + O');
    expect(btn('Global hotkey').textContent).toContain('Capture');
    expect(btn('Global hotkey').querySelector('svg')).toBeTruthy();
    expect(host.querySelector('input[aria-label="Global hotkey"]')).toBeNull();
  });

  it('records a combo, saves it, and mutes global hotkeys only while recording', async () => {
    await start('Global hotkey');
    expect(value('Global hotkey')).toBe('Press a key… (Esc to cancel)');
    expect(mock.hotkeysMuted).toBe(true);
    key('keydown', 'ControlLeft', 'Control', { ctrlKey: true });
    key('keydown', 'AltLeft', 'Alt', { ctrlKey: true, altKey: true });
    key('keydown', 'F8', 'F8', { ctrlKey: true, altKey: true });
    await settle();
    expect(value('Global hotkey')).toBe('Ctrl + Alt + F8');
    expect(store.settings?.global_hotkey).toBe('Ctrl + Alt + F8');
    expect(mock.hotkeysMuted).toBe(false);
    key('keyup', 'F8'); key('keyup', 'AltLeft'); key('keyup', 'ControlLeft');
    await settle();
    expect(store.settings?.global_hotkey).toBe('Ctrl + Alt + F8');
  });

  it.each([
    ['ControlRight', 'RCtrl'], ['ShiftLeft', 'LShift'], ['AltRight', 'RAlt'], ['ControlLeft', 'LCtrl'], ['ShiftRight', 'RShift'], ['AltLeft', 'LAlt'],
  ])('a modifier tapped alone is recorded with its side: %s -> %s', async (code, want) => {
    await store.updateSettings({ global_hotkey: 'F9' }); // free RCtrl (the default) so it is not a duplicate
    await settle();
    await start('Capture hotkey');
    key('keydown', code);
    await settle();
    expect(value('Capture hotkey')).toBe('Press a key… (Esc to cancel)'); // nothing fires on press, only on release
    key('keyup', code);
    await settle();
    expect(store.settings?.capture_hotkey).toBe(want);
    expect(value('Capture hotkey')).toBe(want);
    expect(mock.hotkeysMuted).toBe(false);
  });

  it('Esc cancels, leaves the value unchanged and restores hotkeys', async () => {
    await start('Overlay hotkey');
    tap('Escape');
    await settle();
    expect(value('Overlay hotkey')).toBe('Ctrl + O');
    expect(store.settings?.overlay_hotkey).toBe('Ctrl + O');
    expect(mock.hotkeysMuted).toBe(false);
    expect(mock.calls.filter((c) => c.method === 'settings.update')).toHaveLength(0);
  });

  it('a conflict with another of the three hotkeys keeps the old value and shows a message', async () => {
    await start('Global hotkey');
    key('keydown', 'KeyO', 'o', { ctrlKey: true });
    await settle();
    expect(row('Global hotkey').querySelector('[role="alert"]')?.textContent).toMatch(/already used/);
    expect(value('Global hotkey')).toBe('RCtrl');
    expect(store.settings?.global_hotkey).toBe('RCtrl');
    expect(mock.hotkeysMuted).toBe(false);
    await start('Global hotkey'); // retrying clears the message
    expect(row('Global hotkey').querySelector('[role="alert"]')).toBeNull();
  });

  it('a registration failure (another app owns it) keeps the old value and shows a message', async () => {
    await start('Global hotkey');
    key('keydown', 'F12', 'F12', { ctrlKey: true, altKey: true });
    await settle();
    expect(row('Global hotkey').querySelector('[role="alert"]')?.textContent).toMatch(/another app/i);
    expect(store.settings?.global_hotkey).toBe('RCtrl');
    expect(store.error).toBe('');
    expect(mock.hotkeysMuted).toBe(false);
  });

  it('blur, a host-side end, and teardown all restore the hotkeys', async () => {
    await start('Global hotkey');
    btn('Global hotkey').dispatchEvent(new FocusEvent('blur'));
    await settle();
    expect(mock.hotkeysMuted).toBe(false);
    expect(value('Global hotkey')).toBe('RCtrl');

    await start('Global hotkey');
    mock.push('settings.capture.ended', { capture_id: mock.calls.length, reason: 'blur' });
    await start('Overlay hotkey');
    expect(mock.hotkeysMuted).toBe(true);
    unmount(cmp!); cmp = undefined;
    await settle();
    expect(mock.hotkeysMuted).toBe(false);
  });

  it('the recording state does not change the row layout structure', async () => {
    const before = row('Global hotkey').querySelector('.hk-rec')!;
    const kids = before.children.length;
    await start('Global hotkey');
    const during = row('Global hotkey').querySelector('.hk-rec')!;
    expect(during).toBe(before);
    expect(during.children.length).toBe(kids);
    expect(row('Global hotkey').querySelector('.msg-float')).toBeNull();
  });
});
