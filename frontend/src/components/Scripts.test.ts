import { beforeEach, describe, expect, it } from 'vitest';
import { flushSync, mount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import Scripts from './Scripts.svelte';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;

const count = (m: string) => mock.calls.filter((c) => c.method === m).length;
const settle = async () => { await Promise.resolve(); await Promise.resolve(); flushSync(); };

beforeEach(async () => {
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  mount(Scripts, { target: host, props: { store } });
  flushSync();
});

describe('checkbox hit target', () => {
  it('clicking the label cell around a script checkbox toggles it once and does not select the row', async () => {
    const row = host.querySelector<HTMLElement>('.tr.row')!;
    const id = store.scripts[0].id;
    const before = store.scripts[0].enabled;
    const hit = row.querySelector<HTMLElement>('label.hit')!;
    hit.click();
    await settle();
    expect(count('scripts.setEnabled')).toBe(1);
    expect(store.scripts.find((s) => s.id === id)!.enabled).toBe(!before);
    expect(store.selectedId).toBeNull();
  });

  it('clicking the checkbox itself still toggles once without selecting', async () => {
    const cb = host.querySelector<HTMLInputElement>('.tr.row input.cb')!;
    cb.click();
    await settle();
    expect(count('scripts.setEnabled')).toBe(1);
    expect(store.selectedId).toBeNull();
  });

  it('clicking the name selects the row and does not toggle the checkbox', async () => {
    const row = host.querySelector<HTMLElement>('.tr.row')!;
    row.querySelector<HTMLElement>('.name')!.click();
    await settle();
    expect(store.selectedId).toBe(store.scripts[0].id);
    expect(count('scripts.setEnabled')).toBe(0);
  });

  it('clicking the label cell of a group checkbox toggles the whole group once', async () => {
    host.querySelector<HTMLElement>('.tr.group label.hit')!.click();
    await settle();
    expect(count('scripts.setGroupEnabled')).toBe(1);
    expect(count('scripts.setEnabled')).toBe(0);
  });
});
