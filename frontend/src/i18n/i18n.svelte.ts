import { catalog, parseLocale, type Entry, type Locale, type Text } from './catalog';
import type { MsgId, MsgParams } from './ids.gen';

declare global {
  interface Window {
    __lang?: string;
  }
}

export type Params = Record<string, string | number>;
type Args<K extends MsgId> = keyof MsgParams[K] extends never ? [] : [MsgParams[K]];

function interpolate(s: string, p: Params | undefined): string {
  if (!p || !s.includes('{')) return s;
  return s.replace(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g, (m, k: string) => (k in p ? String(p[k]) : m));
}

function pick(t: Text | null | undefined, lang: Locale, p: Params | undefined): string {
  if (t == null) return '';
  if (typeof t === 'string') return t;
  const n = typeof p?.n === 'number' ? p.n : Number(p?.n ?? 0);
  const rule = lang === 'en' ? new Intl.PluralRules('en').select(n) : 'other';
  return t[rule] ?? t.other ?? '';
}

/** Pure renderer: the lang text, else the English text, else the id itself. */
export function render(lang: Locale, id: string, p?: Params): string {
  const e: Entry | undefined = catalog[id];
  if (!e) return id;
  const text = pick(e[lang], lang, p) || pick(e.en, 'en', p);
  return text ? interpolate(text, p) : id;
}

class I18n {
  lang = $state<Locale>(parseLocale(typeof window === 'undefined' ? undefined : window.__lang) ?? 'en');

  set(l: Locale): void {
    this.lang = l;
    if (typeof document !== 'undefined') document.documentElement.lang = l;
  }
}

export const i18n = new I18n();

/** Reactive translation: reads i18n.lang, so templates and $derived values re-render on a switch. */
export function t<K extends MsgId>(id: K, ...p: Args<K>): string {
  return render(i18n.lang, id, p[0] as Params | undefined);
}
