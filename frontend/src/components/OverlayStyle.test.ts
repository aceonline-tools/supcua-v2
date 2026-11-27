import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from '../lib/store.svelte';
import { ApiError } from '../api/client';
import OverlayStyle from './OverlayStyle.svelte';

let mock: MockBackend;
let store: AppStore;
let host: HTMLElement;
let cmp: ReturnType<typeof mount> | undefined;

const calls = (m: string) => mock.calls.filter((c) => c.method === m);
const settle = async () => { for (let i = 0; i < 8; i++) { await Promise.resolve(); flushSync(); } };
const field = (label: string) => host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
const setSlider = (label: string, v: number) => { const el = field(label); el.value = String(v); el.dispatchEvent(new Event('input', { bubbles: true })); flushSync(); };
async function typeAndCommit(label: string, v: string) {
  const el = field(label);
  el.value = v;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  flushSync();
  el.dispatchEvent(new Event('blur'));
  await settle();
}

beforeEach(async () => {
  vi.useFakeTimers();
  host = document.createElement('div');
  document.body.replaceChildren(host);
  mock = new MockBackend();
  store = new AppStore(createClient(mock));
  await store.init();
  cmp = mount(OverlayStyle, { target: host, props: { store } });
  flushSync();
});
afterEach(() => { if (cmp) unmount(cmp); cmp = undefined; vi.useRealTimers(); });

describe('overlay style settings', () => {
  it('shows the defaults (today\'s overlay)', () => {
    expect(field('Width').value).toBe('136');
    expect(field('Height').value).toBe('32');
    expect(field('Background hex').value).toBe('#404040');
    expect(field('Text hex').value).toBe('');
    expect(host.querySelector('[data-testid="bg-opacity-label"]')!.textContent).toBe('100%');
    expect(host.querySelector('[data-testid="opacity-label"]')!.textContent).toBe('78%');
    expect(host.textContent).toContain("primary monitor's top-left");
  });

  it('applies slider changes live, debounced into one setter call', async () => {
    setSlider('Background opacity', 80);
    setSlider('Background opacity', 60);
    setSlider('Background opacity', 50);
    expect(host.querySelector('[data-testid="bg-opacity-label"]')!.textContent).toBe('50%');
    expect(calls('overlay.setStyle')).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(130);
    await settle();
    expect(calls('overlay.setStyle')).toHaveLength(1);
    expect(calls('overlay.setStyle')[0].params).toMatchObject({ background_opacity: 0.5, width: 136, radius: 0 });
  });

  it('commits valid numbers and colours through the setter', async () => {
    await typeAndCommit('Corner radius', '12');
    await typeAndCommit('Background hex', '#112233');
    await typeAndCommit('Text hex', '#ffeedd');
    await typeAndCommit('X', '-200');
    await vi.advanceTimersByTimeAsync(130);
    await settle();
    const last = calls('overlay.setStyle').at(-1)!.params as Record<string, unknown>;
    expect(last).toMatchObject({ radius: 12, background: '#112233', text_color: '#FFEEDD', x: -200 });
    expect(store.settings!.overlay_style.radius).toBe(12);
  });

  it('rejects invalid input in the UI without calling the backend', async () => {
    await typeAndCommit('Corner radius', '17'); // max is min(136,32)/2 = 16
    await typeAndCommit('Width', '79');
    await typeAndCommit('Height', '300');
    await typeAndCommit('X', '1.5');
    await typeAndCommit('Background hex', 'red');
    await typeAndCommit('Text hex', '#12');
    await vi.advanceTimersByTimeAsync(200);
    await settle();
    expect(calls('overlay.setStyle')).toHaveLength(0);
    for (const l of ['Corner radius', 'Width', 'Height', 'X', 'Background hex', 'Text hex']) expect(field(l).getAttribute('aria-invalid')).toBe('true');
    expect(store.settings!.overlay_style.width).toBe(136);
  });

  it('shrinking the size also lowers the radius to its new limit', async () => {
    await typeAndCommit('Corner radius', '16');
    await typeAndCommit('Height', '24');
    await vi.advanceTimersByTimeAsync(130);
    await settle();
    expect(store.settings!.overlay_style).toMatchObject({ height: 24, radius: 12 });
  });

  it('reset restores the defaults through the backend', async () => {
    await typeAndCommit('Corner radius', '10');
    await typeAndCommit('Width', '300');
    await vi.advanceTimersByTimeAsync(130);
    await settle();
    host.querySelector<HTMLButtonElement>('button')!.click();
    await settle();
    expect(calls('overlay.resetStyle')).toHaveLength(1);
    expect(field('Width').value).toBe('136');
    expect(field('Corner radius').value).toBe('0');
    expect(field('Background hex').value).toBe('#404040');
  });

  it('rolls back to the last confirmed style when the backend rejects', async () => {
    const orig = mock.call.bind(mock);
    mock.call = async (m: string, p: unknown) => {
      if (m === 'overlay.setStyle') throw new ApiError({ code: 'SAVE_FAILED', message: 'Could not save' });
      return orig(m, p);
    };
    await typeAndCommit('Width', '300');
    expect(field('Width').value).toBe('300');
    await vi.advanceTimersByTimeAsync(130);
    await settle();
    expect(store.settings!.overlay_style.width).toBe(136);
    expect(field('Width').value).toBe('136');
    expect(store.error).toBeTruthy();
  });

  it('an external settings.changed (overlay dragged) updates the X/Y fields', async () => {
    (mock as unknown as { seed: { settings: { overlay_style: { x: number; y: number } } } }).seed.settings.overlay_style.x = 640;
    (mock as unknown as { seed: { settings: { overlay_style: { x: number; y: number } } } }).seed.settings.overlay_style.y = 360;
    mock.push('settings.changed', { fields: ['overlay_style'] });
    await settle();
    expect(field('X').value).toBe('640');
    expect(field('Y').value).toBe('360');
  });

  it('keeps an in-flight local edit when an external settings.changed arrives', async () => {
    setSlider('Overall opacity', 50);
    mock.push('settings.changed', { fields: ['overlay_style'] });
    await settle();
    expect(host.querySelector('[data-testid="opacity-label"]')!.textContent).toBe('50%');
  });
});
