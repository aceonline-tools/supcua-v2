import { ApiError } from '../api/client';
import type { ErrorCode } from '../api/types';
import { catalog } from './catalog';
import { i18n, render, type Params } from './i18n.svelte';

/** A retained error: stores keep this, never English text, and translate when rendering. */
export interface ErrorRef {
  code: ErrorCode;
  field?: string;
  params?: Params;
  /** The server's English message: used only when the catalogue has no entry for the code. */
  fallback?: string;
}

export function toErrorRef(e: unknown): ErrorRef {
  if (e instanceof ApiError) return { code: e.code, field: e.body.field, fallback: e.message };
  return { code: 'INTERNAL' };
}

export function errorId(ref: ErrorRef): string {
  const base = `error.${ref.code.toLowerCase()}`;
  const withField = ref.field ? `${base}.${ref.field}` : '';
  if (withField && withField in catalog) return withField;
  return base in catalog ? base : '';
}

/** Reads i18n.lang, so a template or $derived using it re-renders on a language switch. */
export function errorMessage(ref: ErrorRef): string {
  const id = errorId(ref);
  if (id) return render(i18n.lang, id, ref.params);
  return ref.fallback || render(i18n.lang, 'error.internal');
}
