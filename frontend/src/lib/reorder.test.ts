import { describe, expect, it } from 'vitest';
import { dropIndex, gapAt, moveItem, scrollStep } from './reorder';

const L = ['a', 'b', 'c', 'd'];

describe('moveItem', () => {
  it('moves down (drop before a later row)', () => expect(moveItem(L, 0, 3)).toEqual(['b', 'c', 'a', 'd']));
  it('moves up (drop before an earlier row)', () => expect(moveItem(L, 3, 1)).toEqual(['a', 'd', 'b', 'c']));
  it('first to last', () => expect(moveItem(L, 0, 4)).toEqual(['b', 'c', 'd', 'a']));
  it('last to first', () => expect(moveItem(L, 3, 0)).toEqual(['d', 'a', 'b', 'c']));
  it('drop onto itself is a no-op', () => {
    expect(moveItem(L, 1, 1)).toEqual(L);
    expect(moveItem(L, 1, 2)).toEqual(L);
    expect(dropIndex(1, 1)).toBe(1);
    expect(dropIndex(1, 2)).toBe(1);
  });
  it('does not mutate and ignores bad indices', () => {
    const src = ['a', 'b'];
    moveItem(src, 0, 2);
    expect(src).toEqual(['a', 'b']);
    expect(moveItem(src, 5, 0)).toEqual(['a', 'b']);
  });
});

describe('gapAt / scrollStep', () => {
  const rects = [0, 24, 48].map((top) => ({ top, bottom: top + 24 }));
  it('picks the nearest gap by row midpoints', () => {
    expect(gapAt(5, rects)).toBe(0);
    expect(gapAt(13, rects)).toBe(1);
    expect(gapAt(40, rects)).toBe(2);
    expect(gapAt(500, rects)).toBe(3);
  });
  it('scrolls only inside the 24px edge zones', () => {
    expect(scrollStep(200, 100, 400)).toBe(0);
    expect(scrollStep(105, 100, 400)).toBeLessThan(0);
    expect(scrollStep(395, 100, 400)).toBeGreaterThan(0);
  });
});
