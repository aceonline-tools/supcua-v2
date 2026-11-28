import { describe, expect, it } from 'vitest';
import { intersects, isClipped, newInVietnamese, outside, scanLayout } from './layoutScan';

const m = (o: Partial<Parameters<typeof isClipped>[0]>) => ({ scrollWidth: 100, clientWidth: 100, scrollHeight: 20, clientHeight: 20, overflowX: 'hidden', overflowY: 'hidden', ...o });

describe('layoutScan geometry', () => {
  it('detects clipped text only when the box clips', () => {
    expect(isClipped(m({}))).toBe(false);
    expect(isClipped(m({ scrollWidth: 140 }))).toBe(true);
    expect(isClipped(m({ scrollHeight: 30 }))).toBe(true);
    expect(isClipped(m({ scrollWidth: 140, overflowX: 'visible' }))).toBe(false);
    expect(isClipped(m({ scrollWidth: 101 }))).toBe(false);
  });
  it('detects outside and intersecting boxes', () => {
    const o = { left: 0, top: 0, right: 100, bottom: 20 };
    expect(outside({ left: 0, top: 0, right: 101, bottom: 20 }, o)).toBe(false);
    expect(outside({ left: 0, top: 0, right: 130, bottom: 20 }, o)).toBe(true);
    expect(intersects(o, { left: 50, top: 5, right: 150, bottom: 15 })).toBe(true);
    expect(intersects(o, { left: 100, top: 0, right: 150, bottom: 20 })).toBe(false);
  });
  it('counts only violations new in Vietnamese', () => {
    const a = { kind: 'clipped-text', selector: 'p.a', detail: '' } as const;
    const b = { kind: 'clipped-text', selector: 'p.b', detail: '' } as const;
    expect(newInVietnamese([a], [a, b])).toEqual([b]);
  });
  it('runs on a document without throwing', () => {
    document.body.innerHTML = '<button aria-label="Go">Go</button><p>text</p>';
    expect(Array.isArray(scanLayout(window))).toBe(true);
  });
});
