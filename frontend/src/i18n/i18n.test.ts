import { describe, expect, it, beforeEach } from 'vitest';
import { flushSync } from 'svelte';
import { catalog, parseLocale } from './catalog';
import { errorId, errorMessage, toErrorRef } from './errors';
import { i18n, render, t } from './i18n.svelte';
import { ApiError } from '../api/client';
import { generate } from '../../../scripts/i18n-gen.mjs';
import idsGen from './ids.gen.ts?raw';
import migratedFiles from './migrated.json';
import allowlist from '../../i18n-allow.json';
import { applyAllowlist, scanSource, validateCatalog, type AllowEntry } from './lint';

const raw = import.meta.glob(['../**/*.ts', '../**/*.svelte'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>;
const sources: Record<string, string> = Object.fromEntries(Object.entries(raw).map(([p, v]) => [p.startsWith('./') ? `i18n/${p.slice(2)}` : p.replace(/^\.\.\//, ''), v]));
const source = (rel: string): string => sources[rel.replace(/^src\//, '')];

describe('catalogue parity (contract 7.1)', () => {
  it('has no problems', () => {
    const { problems } = validateCatalog(catalog);
    expect(problems).toEqual([]);
  });

  it('fails when a translation is missing or wrong (negative proofs)', () => {
    const mut = (f: (c: typeof catalog) => void) => {
      const c = structuredClone(catalog);
      f(c);
      return validateCatalog(c).problems;
    };
    expect(mut((c) => { c['tray.exit'].vi = ''; }).length).toBeGreaterThan(0);
    expect(mut((c) => { delete (c['tray.exit'] as Partial<typeof c[string]>).vi; }).length).toBeGreaterThan(0);
    expect(mut((c) => { c['tray.exit'].vi = c['tray.exit'].en; }).length).toBeGreaterThan(0);
    expect(mut((c) => { c['tray.tooltipRunning'].vi = 'supcua-v2'; }).length).toBeGreaterThan(0);
    expect(mut((c) => { c['x.n'] = { en: { one: '{n} a', other: '{n} as' }, vi: { one: '{n} b' } }; }).length).toBeGreaterThan(0);
  });

  it('keeps ids.gen.ts fresh', () => {
    expect(idsGen).toBe(generate(catalog));
  });

  it('references only existing ids', () => {
    const missing: string[] = [];
    for (const [f, src] of Object.entries(sources)) {
      if (/\.test\.ts$|ids\.gen\.ts$|lint\.ts$/.test(f)) continue;
      for (const m of src.matchAll(/(?:^|[^\w.$])t\(\s*['"`]([A-Za-z][\w.]*)['"`]/g)) if (!(m[1] in catalog)) missing.push(`${f}: ${m[1]}`);
    }
    expect(missing).toEqual([]);
  });
});

describe('runtime', () => {
  beforeEach(() => i18n.set('en'));

  it('renders both languages, params, plurals and falls back to the id', () => {
    expect(render('en', 'tray.exit')).toBe('Exit');
    expect(render('vi', 'tray.exit')).toBe('Thoát');
    expect(render('vi', 'tray.tooltipRunning', { script: 'Farm' })).toBe('supcua-v2 - Đang bật - Đang chạy: Farm');
    expect(render('en', 'no.such.id')).toBe('no.such.id');
    const c = catalog as Record<string, unknown>;
    c['x.count'] = { en: { one: '{n} script', other: '{n} scripts' }, vi: { other: '{n} script' } };
    expect(render('en', 'x.count', { n: 1 })).toBe('1 script');
    expect(render('en', 'x.count', { n: 3 })).toBe('3 scripts');
    expect(render('vi', 'x.count', { n: 3 })).toBe('3 script');
    delete c['x.count'];
  });

  it('t() is reactive to the language', () => {
    expect(t('settings.language')).toBe('Language');
    i18n.set('vi');
    flushSync();
    expect(t('settings.language')).toBe('Ngôn ngữ');
    expect(document.documentElement.lang).toBe('vi');
  });

  it('parses locales like the Go side', () => {
    expect(parseLocale(' VI-vn ')).toBe('vi');
    expect(parseLocale('fr')).toBeNull();
    expect(parseLocale(5)).toBeNull();
  });
});

describe('retained errors re-render on a language switch', () => {
  it('translates at presentation time, with fallbacks', () => {
    i18n.set('en');
    const ref = toErrorRef(new ApiError({ code: 'NAME_TAKEN', message: 'server english' }));
    expect(errorMessage(ref)).toBe('a script with this name already exists');
    i18n.set('vi');
    expect(errorMessage(ref)).toBe('Đã có script trùng tên');
    expect(errorMessage(toErrorRef(new Error('boom')))).toBe('Lỗi nội bộ');
    const unknown = { code: 'FUTURE_CODE' as never, fallback: 'server text' };
    expect(errorId(unknown)).toBe('');
    expect(errorMessage(unknown)).toBe('server text');
    expect(errorMessage({ code: 'FUTURE_CODE' as never })).toBe('Lỗi nội bộ');
    i18n.set('en');
  });
});

describe('literal scan (contract 7.2)', () => {
  const scan = (f: string, s: string) => scanSource(f, s).map((h) => h.text);

  it('flags text nodes, accessible attributes, expression literals and named fields', () => {
    expect(scan('a.svelte', '<p>Hello world</p>')).toContain('Hello world');
    expect(scan('a.svelte', '<button aria-label="Close">x</button>')).toContain('Close');
    expect(scan('a.svelte', '<input title="Pick one" placeholder="auto" />')).toEqual(['Pick one', 'auto']);
    expect(scan('a.svelte', "<p>{ok ? 'Saved changes' : t('x.y')}</p>")).toEqual(['Saved changes']);
    expect(scan('a.svelte', "<button aria-label={on ? 'Stop' : 'Go'}></button>")).toEqual(['Stop', 'Go']);
    expect(scan('a.svelte', '<script lang="ts">\n  let error = $state("");\n  error = \'Something bad happened\';\n  const label = \'Dark\';\n</script>')).toEqual(['Something bad happened', 'Dark']);
    expect(scan('s.ts', "export const errText = { a: 'Update failed' };\nfunction statusText() { return 'Idle'; }")).toEqual(['Update failed', 'Idle']);
  });

  it('ignores ids, css, calls to t(), types, comparisons, logs and imports', () => {
    expect(scan('a.svelte', '<script lang="ts">\n  import x from "./y";\n  type K = "Hello World";\n  const cls = "active";\n  console.warn("Something went wrong here");\n  if (k === "Escape") {}\n</script>\n<p class="x y">{t(\'a.b\')}</p>')).toEqual([]);
  });

  it('keeps the migrated files clean against the reviewed allowlist, and the allowlist honest', () => {
    const migrated: string[] = migratedFiles;
    const allow = allowlist as AllowEntry[];
    for (const f of migrated) expect(source(f), `${f} must exist`).toBeTypeOf('string');
    const hits = migrated.flatMap((f) => scanSource(f, source(f)));
    const r = applyAllowlist(hits, allow, migrated);
    expect(r.unlisted).toEqual([]);
    expect(r.stale).toEqual([]);
    expect(r.badReason).toEqual([]);
  });

  it('fails on an unlisted literal, a stale entry and a bad reason (negative proofs)', () => {
    const hits = scanSource('src/components/X.svelte', '<p>Brand new text</p>');
    expect(applyAllowlist(hits, [], ['src/components/X.svelte']).unlisted).toHaveLength(1);
    const stale: AllowEntry = { file: 'src/components/X.svelte', match: 'gone', reason: 'brand' };
    expect(applyAllowlist(hits, [stale], ['src/components/X.svelte']).stale).toEqual([stale]);
    const bad: AllowEntry = { file: 'src/components/X.svelte', match: 'Brand new text', reason: 'because' };
    expect(applyAllowlist(hits, [bad], ['src/components/X.svelte']).badReason).toEqual([bad]);
  });
});
