import raw from '../../../locale/catalog.json';

export type Locale = 'en' | 'vi';
export const LOCALES: readonly Locale[] = ['en', 'vi'];
export type Text = string | Record<string, string>;
export interface Entry {
  en: Text;
  vi: Text;
  scope?: string[];
  tight?: boolean;
  ctx?: string;
  same?: boolean;
  why?: string;
  pending?: string;
}
export type CatalogData = Record<string, Entry>;

export const catalog = raw as unknown as CatalogData;

/** Same normalisation as Go locale.Parse: trim, lower-case, primary subtag. */
export function parseLocale(v: unknown): Locale | null {
  if (typeof v !== 'string') return null;
  const s = v.trim().toLowerCase().split(/[-_]/)[0];
  return (LOCALES as readonly string[]).includes(s) ? (s as Locale) : null;
}
