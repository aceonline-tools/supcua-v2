import { describe, expect, it, vi, beforeEach } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import LanguageSetting from './LanguageSetting.svelte';
import { i18n } from '../i18n';
import { ApiError } from '../api/client';

function fakeStore(language: 'en' | 'vi', call = vi.fn().mockResolvedValue({})) {
  return { settings: { language }, api: { call }, error: '' } as never;
}

const pick = (s: HTMLSelectElement, v: string) => { s.value = v; s.dispatchEvent(new Event('change', { bubbles: true })); };

describe('LanguageSetting', () => {
  beforeEach(() => { document.body.innerHTML = ''; i18n.set('en'); });

  it('shows the chosen language, switches the UI at once and persists through settings.update', async () => {
    const call = vi.fn().mockResolvedValue({});
    const c = mount(LanguageSetting, { target: document.body, props: { store: fakeStore('en', call) } });
    flushSync();
    expect(document.body.querySelector('strong')?.textContent).toBe('Language');
    const select = document.body.querySelector('select')!;
    expect([...select.options].map((o) => o.textContent)).toEqual(['English', 'Tiếng Việt']);
    pick(select, 'vi');
    flushSync();
    expect(call).toHaveBeenCalledWith('settings.update', { patch: { language: 'vi' } });
    expect(document.body.querySelector('strong')?.textContent).toBe('Ngôn ngữ');
    expect(select.value).toBe('vi');
    expect(select.getAttribute('aria-label')).toBe('Ngôn ngữ');
    unmount(c);
  });

  it('adopts the persisted language from settings and reverts when saving fails', async () => {
    const c = mount(LanguageSetting, { target: document.body, props: { store: fakeStore('vi') } });
    flushSync();
    expect(i18n.lang).toBe('vi');
    unmount(c);

    i18n.set('en');
    const store = fakeStore('en', vi.fn().mockRejectedValue(new ApiError({ code: 'SAVE_FAILED', message: 'could not write script file' })));
    const c2 = mount(LanguageSetting, { target: document.body, props: { store } });
    flushSync();
    pick(document.body.querySelector('select')!, 'vi');
    await new Promise((r) => setTimeout(r, 0));
    flushSync();
    expect(i18n.lang).toBe('en');
    expect((store as unknown as { error: string }).error).toBe('could not write script file');
    unmount(c2);
  });
});
