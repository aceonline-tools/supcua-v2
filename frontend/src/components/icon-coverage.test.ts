import { describe, expect, it } from 'vitest';
import { findButtons, scanSource } from '../lib/icon-scan';

const sources = import.meta.glob<string>('../**/*.svelte', { query: '?raw', import: 'default', eager: true });
const rel = (p: string) => (p.startsWith('./') ? 'components/' + p.slice(2) : p.replace(/^(\.\.\/)+/, ''));
const byFile = Object.fromEntries(Object.entries(sources).map(([p, src]) => [rel(p), src]));
const files = Object.keys(byFile);

// Files whose buttons intentionally carry no icon. Each needs a reason.
const ALLOW: Record<string, string> = {
  // TODO(phase 2): remove this entry once Settings.svelte buttons get icons.
  'components/Settings.svelte': 'phase 1: Settings is done in phase 2',
};

describe('every button has an icon (static source scan)', () => {
  for (const f of files) {
    it(f, () => {
      if (ALLOW[f]) return;
      expect(scanSource(byFile[f]).map((x) => `${f}:${x.line} ${x.problem}`)).toEqual([]);
    });
  }

  it('allow-list entries point at existing files', () => {
    for (const f of Object.keys(ALLOW)) expect(files).toContain(f);
  });

  it('scanner sanity: scans nested paths and finds many buttons', () => {
    expect(files).toContain('App.svelte');
    const n = files.filter((f) => !ALLOW[f]).reduce((a, f) => a + findButtons(byFile[f]).length, 0);
    expect(n).toBeGreaterThan(40);
  });
});
