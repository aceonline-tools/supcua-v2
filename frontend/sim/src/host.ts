import type { EditorBounds } from './sizing';

/** A message is trusted only from the window's own contentWindow AND its expected origin. */
export const trustedMessage = (e: { source: unknown; origin: string }, win: { source: unknown; origin: string }): boolean =>
  e.source !== null && e.source === win.source && e.origin === win.origin;

export interface MeasureState { token: number; retries: number }
export type MeasureDecision =
  | { kind: 'stale'; token: number; cssW: number }
  | { kind: 'fallback' }
  | { kind: 'remeasure'; token: number; cssW: number }
  | { kind: 'show' };

const inRange = (n: unknown): boolean => Number.isInteger(n) && (n as number) >= 1 && (n as number) <= 8192;
export const MAX_REMEASURES = 2;

/** The host's answer to editor.measured (contract §5). A stale token never resizes or shows the window. */
export function decideMeasure(state: MeasureState, p: { token: number; cssW: number; cssH: number }, bounds: (() => EditorBounds) | null): MeasureDecision {
  if (!inRange(p.token) || !inRange(p.cssW) || !inRange(p.cssH)) return { kind: 'fallback' };
  if (p.token !== state.token) return { kind: 'stale', token: state.token, cssW: p.cssW };
  const b = bounds?.();
  if (b && b.widthClamped && !b.fromMemory && state.retries < MAX_REMEASURES) return { kind: 'remeasure', token: state.token + 1, cssW: b.remeasureCssW };
  return { kind: 'show' };
}
