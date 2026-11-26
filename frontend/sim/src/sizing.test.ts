import { describe, expect, it } from 'vitest';
import {
  RULES, clientPhysical, cssViewport, fitRect, frameMetrics, isDefaultSize, physicalDelta, resizeRect, scaleDPI, scaledMin,
  taskbarHeight, useDefaultSize, workArea, type Rect,
} from './sizing';

const r = (L: number, T: number, R: number, B: number): Rect => ({ L, T, R, B });

describe('scaleDPI (dpisize_test.go TestScaleDPI, v1.2.3 and v1.3.0)', () => {
  it.each([[1000, 96, 1000], [1000, 120, 1250], [1000, 144, 1500], [700, 192, 1400], [640, 0, 640]])('%i @ %i dpi = %i', (v, dpi, want) => {
    expect(scaleDPI(v, dpi)).toBe(want);
  });
});

describe('scaledMin (TestScaledMinCappedToWorkArea, identical in v1.2.3 and v1.3.0)', () => {
  it('caps to the work area', () => {
    expect(scaledMin(192, 640, 420, 1200, 800)).toEqual([1200, 800]);
    expect(scaledMin(144, 640, 420, 1920, 1040)).toEqual([960, 630]);
    expect(scaledMin(144, 640, 420, 0, 0)).toEqual([960, 630]);
  });
});

type Case = [string, number, Rect, number, number, number, number, number, number];
const fit = (cases: Case[], dh: number) =>
  it.each(cases)('%s', (_n, dpi, work, x, y, wx, wy, ww, wh) => {
    expect(fitRect(dpi, work, x, y, 640, 420, 1000, dh)).toEqual([wx, wy, ww, wh]);
  });

describe('fitRect v1.2.3 vectors (default 1000x700)', () => {
  fit([
    ['100% roomy', 96, r(0, 0, 1920, 1040), 100, 100, 100, 100, 1000, 700],
    ['150% roomy', 144, r(0, 0, 2560, 1400), 50, 50, 50, 50, 1500, 1050],
    ['150% 1080p cascade origin', 144, r(0, 0, 1920, 1040), 60, 60, 60, 0, 1500, 1040],
    ['200% on 1200x800', 192, r(0, 0, 1200, 800), 100, 100, 0, 0, 1200, 800],
    ['positive origin near bottom/right', 144, r(0, 0, 1920, 1040), 1800, 900, 420, 0, 1500, 1040],
    ['secondary monitor offset', 96, r(1920, 0, 3840, 1040), 3700, 600, 2840, 340, 1000, 700],
    ['unknown work area', 144, r(0, 0, 0, 0), 10, 10, 10, 10, 1500, 1050],
  ], 700);
  it('isDefaultSize', () => {
    expect(isDefaultSize(0, 0) && isDefaultSize(1000, 700) && !isDefaultSize(1000, 701) && !isDefaultSize(1500, 1050)).toBe(true);
  });
});

describe('fitRect v1.3.0 vectors (default 1000x960)', () => {
  fit([
    ['100% roomy', 96, r(0, 0, 1920, 1040), 100, 100, 100, 80, 1000, 960],
    ['150% roomy', 144, r(0, 0, 2560, 1500), 50, 50, 50, 50, 1500, 1440],
    ['150% 1080p cascade origin', 144, r(0, 0, 1920, 1040), 60, 60, 60, 0, 1500, 1040],
    ['200% on 1200x800', 192, r(0, 0, 1200, 800), 100, 100, 0, 0, 1200, 800],
    ['positive origin near bottom/right', 144, r(0, 0, 1920, 1040), 1800, 900, 420, 0, 1500, 1040],
    ['secondary monitor offset', 96, r(1920, 0, 3840, 1040), 3700, 600, 2840, 80, 1000, 960],
    ['unknown work area', 144, r(0, 0, 0, 0), 10, 10, 10, 10, 1500, 1440],
  ], 960);
  it.each([
    ['no saved size', 0, 0, false, true], ['no saved height', 1000, 0, false, true],
    ['current layout keeps 1000x700', 1000, 700, false, false], ['current layout keeps a scaled size', 1250, 875, false, false],
    ['current layout keeps the new default', 1000, 960, false, false], ['stale layout resets 1000x700', 1000, 700, true, true],
    ['stale layout resets any size', 1337, 811, true, true], ['stale layout resets the new default size', 1000, 960, true, true],
  ] as const)('useDefaultSize: %s', (_n, w, h, reset, want) => {
    expect(useDefaultSize(w, h, reset)).toBe(want);
  });
  it('TestDefaultHeightFitsScriptsContent: 100% client leaves >= 24 CSS px over the 879 px content', () => {
    const [, , w, h] = fitRect(96, r(0, 0, 1920, 1032), 300, 200, 640, 420, 1000, 960);
    expect([w, h]).toEqual([1000, 960]);
    expect(h - 31 - 8).toBeGreaterThanOrEqual(879 + 24);
    for (const dpi of [120, 144]) {
      const ah = 1032 - Math.floor((12 * (dpi - 96)) / 24);
      const [, , fw, fh] = fitRect(dpi, r(0, 0, 1920, ah), 0, 0, 640, 420, 1000, 960);
      expect(fw).toBeLessThanOrEqual(1920);
      expect(fh).toBe(Math.min(scaleDPI(960, dpi), ah));
    }
  });
});

describe('per-version fresh-launch rules', () => {
  const at = (s: 100 | 125 | 150) => ({ dpi: (96 * s) / 100, work: workArea(s) });
  it('v1.2.1 is a fixed 1000x700 with an unscaled 640x420 minimum at every DPI', () => {
    for (const s of [100, 125, 150] as const) {
      const { dpi, work } = at(s);
      expect(RULES['v1.2.1'].initial(dpi, work)).toMatchObject({ w: 1000, h: 700 });
      expect(RULES['v1.2.1'].minSize(dpi, work)).toEqual([640, 420]);
    }
  });
  it('v1.2.3 scales 1000x700 and caps to the work area', () => {
    expect(RULES['v1.2.3'].initial(96, workArea(100))).toMatchObject({ w: 1000, h: 700 });
    expect(RULES['v1.2.3'].initial(120, workArea(125))).toMatchObject({ w: 1250, h: 875 });
    expect(RULES['v1.2.3'].initial(144, workArea(150))).toMatchObject({ w: 1500, h: 1008 });
    expect(RULES['v1.2.3'].minSize(144, workArea(150))).toEqual([960, 630]);
  });
  it('v1.3.0 scales 1000x960 and caps to the work area', () => {
    expect(RULES['v1.3.0'].initial(96, workArea(100))).toMatchObject({ w: 1000, h: 960 });
    expect(RULES['v1.3.0'].initial(120, workArea(125))).toMatchObject({ w: 1250, h: 1020 });
    expect(RULES['v1.3.0'].initial(144, workArea(150))).toMatchObject({ w: 1500, h: 1008 });
  });
});

describe('frame and viewport metrics', () => {
  it('1000x700 at 96 dpi gives the CI-logged client 984x661', () => {
    expect(clientPhysical(1000, 700, 96)).toEqual({ w: 984, h: 661 });
    expect(cssViewport(1000, 700, 96)).toEqual({ w: 984, h: 661 });
  });
  it('border/caption scale with dpi', () => {
    expect(frameMetrics(96)).toEqual({ border: 8, caption: 31 });
    expect(frameMetrics(120)).toEqual({ border: 10, caption: 39 });
    expect(frameMetrics(144)).toEqual({ border: 12, caption: 47 });
  });
  it('CSS viewport = client physical * 96 / dpi', () => {
    expect(cssViewport(1250, 1020, 120)).toEqual({ w: 984, h: 777 });
  });
  it('taskbar heights give the 1032/1020/1008 work areas', () => {
    expect([100, 125, 150].map((s) => workArea(s as 100).B)).toEqual([1032, 1020, 1008]);
    expect(taskbarHeight(150)).toBe(72);
  });
});

describe('resizeRect', () => {
  const start = { x: 100, y: 100, w: 1000, h: 700 };
  it('grows from the south-east corner', () => {
    expect(resizeRect(start, 'se', 50, 40, 640, 420)).toEqual({ x: 100, y: 100, w: 1050, h: 740 });
  });
  it('keeps the opposite edge fixed when the minimum is hit from the north-west', () => {
    const o = resizeRect(start, 'nw', 900, 900, 640, 420);
    expect(o).toEqual({ x: 460, y: 380, w: 640, h: 420 });
    expect(o.x + o.w).toBe(1100);
    expect(o.y + o.h).toBe(800);
  });
  it('rounds fractional deltas to whole px so sizes stay integer', () => {
    const o = resizeRect(start, 'se', 10.6, -3.4, 640, 420);
    expect(o).toEqual({ x: 100, y: 100, w: 1011, h: 697 });
    expect(Object.values(resizeRect(start, 'nw', 7.5, 2.5, 640, 420)).every(Number.isInteger)).toBe(true);
  });
  it('physicalDelta divides by the fit scale and rounds', () => {
    expect(physicalDelta(100, 0.75)).toBe(133);
    expect(physicalDelta(-3, 0.7)).toBe(-4);
    expect(Number.isInteger(physicalDelta(37, 0.913))).toBe(true);
  });
});
