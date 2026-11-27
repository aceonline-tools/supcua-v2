import { describe, expect, it } from 'vitest';

const sources = import.meta.glob<string>('./*.svelte', { query: '?raw', import: 'default', eager: true });
const files = Object.keys(sources).map((p) => p.slice(2));
const read = (f: string) => sources['./' + f];

// Components whose buttons intentionally carry no icon. Each needs a reason.
const ALLOW: Record<string, string> = {
  // Toggle is a text-less role="switch" whose state is shown by the thumb position; an icon would duplicate it.
  'Toggle.svelte': 'switch control, state shown by the thumb',
  // TODO(phase 2): remove this entry once Settings.svelte buttons get icons.
  'Settings.svelte': 'phase 1: Settings is done in phase 2',
};

function elements(src: string): string[] {
  const out: string[] = [];
  for (const m of src.matchAll(/<(button|span|div|a)\b[^>]*>/g)) {
    if (m[1] !== 'button' && !/\brole="button"/.test(m[0])) continue;
    const end = src.indexOf(`</${m[1]}>`, m.index);
    out.push(src.slice(m.index!, end + m[1].length + 3));
  }
  return out;
}

describe('every button has an icon', () => {
  for (const f of files) {
    it(f, () => {
      const els = elements(read(f));
      if (ALLOW[f]) return;
      const bare = els.filter((e) => !/<Icon\b/.test(e)).map((e) => e.split('\n')[0].slice(0, 90));
      expect(bare).toEqual([]);
    });
  }

  it('allow-list entries point at existing files', () => {
    for (const f of Object.keys(ALLOW)) expect(files).toContain(f);
  });

  it('finds buttons (scanner sanity)', () => {
    const n = files.filter((f) => !ALLOW[f]).reduce((a, f) => a + elements(read(f)).length, 0);
    expect(n).toBeGreaterThan(30);
  });
});
