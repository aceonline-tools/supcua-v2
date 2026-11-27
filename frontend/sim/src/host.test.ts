import { describe, expect, it } from 'vitest';
import { decideMeasure, trustedMessage } from './host';
import { editorBounds, workArea } from './sizing';

describe('trustedMessage', () => {
  const win = { source: { id: 1 }, origin: 'http://127.0.0.1:5811' };
  it('accepts the window and its origin only', () => {
    expect(trustedMessage({ source: win.source, origin: win.origin }, win)).toBe(true);
    expect(trustedMessage({ source: win.source, origin: 'http://evil.example' }, win)).toBe(false);
    expect(trustedMessage({ source: { id: 2 }, origin: win.origin }, win)).toBe(false);
    expect(trustedMessage({ source: null, origin: win.origin }, { source: null, origin: win.origin })).toBe(false);
  });
});

describe('decideMeasure', () => {
  const b = (w: number) => () => editorBounds(w, 600, 96, workArea(100), { cx: 900, cy: 500 }, null);
  it('ignores a stale token: no resize, no show', () => {
    const d = decideMeasure({ token: 2, retries: 1 }, { token: 1, cssW: 900, cssH: 600 }, b(900));
    expect(d).toEqual({ kind: 'stale', token: 2, cssW: 900 });
  });
  it('shows on the current token, remeasures when width-clamped within budget, shows when budget is spent', () => {
    expect(decideMeasure({ token: 1, retries: 0 }, { token: 1, cssW: 900, cssH: 600 }, b(900)).kind).toBe('show');
    expect(decideMeasure({ token: 1, retries: 0 }, { token: 1, cssW: 5000, cssH: 600 }, b(5000)).kind).toBe('remeasure');
    expect(decideMeasure({ token: 3, retries: 2 }, { token: 3, cssW: 5000, cssH: 600 }, b(5000)).kind).toBe('show');
  });
  it('out-of-range reports fall back', () => {
    expect(decideMeasure({ token: 1, retries: 0 }, { token: 1, cssW: 0, cssH: 600 }, null).kind).toBe('fallback');
  });
});
