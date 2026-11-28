import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend, seedFromQuery } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import ProcessPicker from './ProcessPicker.svelte';

let host: HTMLElement;
let cmp: ReturnType<typeof mount> | undefined;
let store: AppStore;

const tick = async () => { for (let i = 0; i < 5; i++) { await Promise.resolve(); flushSync(); } };
async function setup(query = '') {
  if (cmp) unmount(cmp);
  host = document.createElement('div');
  document.body.replaceChildren(host);
  store = new AppStore(createClient(new MockBackend(seedFromQuery(query))));
  await store.init();
  cmp = mount(ProcessPicker, { target: host, props: { store, value: 'game.exe' } });
  flushSync();
}
const btn = (label: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!;
const open = async () => { btn('Pick from running processes').click(); await tick(); };
const body = () => host.querySelector('.picker-body')!;

beforeEach(() => setup());

describe('process picker', () => {
  it('is closed by default and does not enumerate until opened', () => {
    expect(host.querySelector('.picker-pop')).toBeNull();
  });

  it('lists rows as text and filters over exe and title, case-insensitively', async () => {
    await open();
    expect(body().querySelectorAll('.picker-row').length).toBe(3);
    const f = host.querySelector<HTMLInputElement>('input[aria-label="Filter processes"]')!;
    f.value = 'CHROME'; f.dispatchEvent(new Event('input', { bubbles: true })); await tick();
    expect(body().querySelectorAll('.picker-row').length).toBe(1);
    f.value = 'notes.TXT'; f.dispatchEvent(new Event('input', { bubbles: true })); await tick();
    expect(body().querySelector('.picker-exe')!.textContent).toBe('Notepad.exe');
    f.value = 'zzz'; f.dispatchEvent(new Event('input', { bubbles: true })); await tick();
    expect(body().textContent).toContain('No matches.');
  });

  it('shows the empty state', async () => {
    await setup('procs=empty');
    await open();
    expect(body().textContent).toContain('No windows found.');
  });

  it('says the list is incomplete when it is empty and truncated', async () => {
    (store.api as unknown as { call: unknown }).call = async () => ({ rows: [], truncated: true });
    await open();
    expect(body().textContent).toContain('List incomplete. Refresh or type a name.');
    expect(body().textContent).not.toContain('No windows found.');
  });

  it('shows the error state in the box and keeps the text field usable', async () => {
    await setup('procs=error');
    await open();
    expect(body().querySelector('[role="alert"]')!.textContent).toContain('still type a name');
    expect(host.querySelector<HTMLInputElement>('input[aria-label="Game process name"]')!.disabled).toBe(false);
  });

  it('shows loading in the same box, then picking saves the exe basename only and closes', async () => {
    await setup('procdelay=30');
    btn('Pick from running processes').click(); flushSync();
    expect(body().textContent).toContain('Loading…');
    await new Promise((r) => setTimeout(r, 60)); await tick();
    body().querySelector<HTMLElement>('.picker-row')!.click();
    await new Promise((r) => setTimeout(r, 20)); await tick();
    expect(host.querySelector('.picker-pop')).toBeNull();
    expect(store.state!.auto_active.process_name).toBe('ACEonline.exe');
  });

  it('closes on Escape', async () => {
    await open();
    host.querySelector('.picker-ctl')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await tick();
    expect(host.querySelector('.picker-pop')).toBeNull();
  });

  it('renders hostile titles as text', async () => {
    await setup('procs=many');
    await open();
    expect(body().querySelectorAll('.picker-row').length).toBe(60);
    expect(body().textContent).toContain('List is partial');
    expect(body().querySelector('img,script')).toBeNull();
  });

  it('returns focus to the trigger after Escape and after a pick', async () => {
    await open();
    host.querySelector('.picker-ctl')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await tick();
    expect(document.activeElement).toBe(btn('Pick from running processes'));
    await open();
    body().querySelector<HTMLElement>('.picker-row')!.click();
    await new Promise((r) => setTimeout(r, 20)); await tick();
    expect(host.querySelector('.picker-pop')).toBeNull();
    expect(document.activeElement).toBe(btn('Pick from running processes'));
  });

  it('a stale pick cannot close a reopened picker, repeat picks are ignored, old errors are not shown', async () => {
    let calls = 0;
    let finish: (v: { ok: boolean }) => void = () => {};
    store.setAutoActive = () => { calls++; return new Promise((r) => { finish = r; }); };
    await open();
    const row = body().querySelector<HTMLElement>('.picker-row')!;
    row.click(); row.click(); await tick();
    expect(calls).toBe(1);
    host.querySelector('.picker-ctl')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await tick();
    await open();
    finish({ ok: false });
    await new Promise((r) => setTimeout(r, 10)); await tick();
    expect(host.querySelector('.picker-pop')).not.toBeNull();
    expect(body().textContent).not.toContain("Couldn't save");
    finish({ ok: true });
  });
});
