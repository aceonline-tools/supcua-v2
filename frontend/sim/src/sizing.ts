// Window sizing rules of the shipped Windows host, ported per release tag. Go sources (line refs are
// from the tag): v1.2.1 ui/webview/ui_windows.go:454-462 (WM_GETMINMAXINFO), :508 (fixed 1000x700),
// main.go:570 (MinW/MinH 640x420); v1.2.3 and v1.3.0 ui/webview/dpisize.go (scaleDPI, scaledMin,
// fitRect) with ui_windows.go fitDefaultSize; v1.3.0 adds useDefaultSize and defaultH 960.

export const BASE_DPI = 96;
export const SCREEN = { w: 1920, h: 1080 } as const;
export const SCALINGS = [100, 125, 150] as const;
export type Scaling = (typeof SCALINGS)[number];
export const VERSIONS = ['v1.2.1', 'v1.2.3', 'v1.3.0'] as const;
export type VersionId = (typeof VERSIONS)[number];

export interface Rect { L: number; T: number; R: number; B: number }
export interface Placement { x: number; y: number; w: number; h: number }

const MIN_W = 640;
const MIN_H = 420;
// Windows' first-window cascade offset (CW_USEDEFAULT); the Go tests use the same 60,60 origin.
export const CASCADE = { x: 60, y: 60 } as const;

export const dpiOf = (scaling: Scaling): number => (BASE_DPI * scaling) / 100;

export function scaleDPI(v: number, dpi: number): number {
  if (dpi <= 0 || dpi === BASE_DPI) return v;
  return Math.floor((v * dpi + BASE_DPI / 2) / BASE_DPI);
}

export function clampInt(v: number, lo: number, hi: number): number {
  if (v > hi) v = hi;
  if (v < lo) v = lo;
  return v;
}

export function scaledMin(dpi: number, minW: number, minH: number, aw: number, ah: number): [number, number] {
  let mw = scaleDPI(minW, dpi);
  let mh = scaleDPI(minH, dpi);
  if (aw > 0 && mw > aw) mw = aw;
  if (ah > 0 && mh > ah) mh = ah;
  return [mw, mh];
}

export function fitRect(
  dpi: number, work: Rect, x: number, y: number, minW: number, minH: number, defaultW: number, defaultH: number,
): [number, number, number, number] {
  const aw = work.R - work.L;
  const ah = work.B - work.T;
  const [mw, mh] = scaledMin(dpi, minW, minH, aw, ah);
  let w = scaleDPI(defaultW, dpi);
  let h = scaleDPI(defaultH, dpi);
  if (aw > 0) w = clampInt(w, mw, aw);
  else if (w < mw) w = mw;
  if (ah > 0) h = clampInt(h, mh, ah);
  else if (h < mh) h = mh;
  if (aw > 0) x = clampInt(x, work.L, work.R - w);
  if (ah > 0) y = clampInt(y, work.T, work.B - h);
  return [x, y, w, h];
}

export const isDefaultSize = (w: number, h: number): boolean => w <= 0 || h <= 0 || (w === 1000 && h === 700);
export const useDefaultSize = (w: number, h: number, reset: boolean): boolean => reset || w <= 0 || h <= 0;

export interface VersionRules {
  id: VersionId;
  /** Minimum outer size in physical px for a window on a work area of the given size. */
  minSize(dpi: number, work: Rect): [number, number];
  /** Where a fresh launch (nothing saved) places the window. */
  initial(dpi: number, work: Rect): Placement;
  note: string;
}

export const RULES: Record<VersionId, VersionRules> = {
  'v1.2.1': {
    id: 'v1.2.1',
    minSize: () => [MIN_W, MIN_H],
    initial: () => ({ ...CASCADE, w: 1000, h: 700 }),
    note: 'fixed 1000x700 physical at every DPI; minimum 640x420 physical, unscaled; no work-area clamp',
  },
  'v1.2.3': {
    id: 'v1.2.3',
    minSize: (dpi, work) => scaledMin(dpi, MIN_W, MIN_H, work.R - work.L, work.B - work.T),
    initial: (dpi, work) => {
      const [x, y, w, h] = fitRect(dpi, work, CASCADE.x, CASCADE.y, MIN_W, MIN_H, 1000, 700);
      return { x, y, w, h };
    },
    note: 'DPI-scaled 1000x700 default and minimum; fitRect clamps to the work area; scaledMin capped',
  },
  'v1.3.0': {
    id: 'v1.3.0',
    minSize: (dpi, work) => scaledMin(dpi, MIN_W, MIN_H, work.R - work.L, work.B - work.T),
    initial: (dpi, work) => {
      const [x, y, w, h] = fitRect(dpi, work, CASCADE.x, CASCADE.y, MIN_W, MIN_H, 1000, 960);
      return { x, y, w, h };
    },
    note: 'DPI-scaled 1000x960 default and minimum; fitRect clamps to the work area; scaledMin capped',
  },
};

// Assumed Windows 11 frame metrics (not read from the Go code): 8 px border and 31 px caption at 96 DPI.
export const frameMetrics = (dpi: number) => ({
  border: Math.round((8 * dpi) / BASE_DPI),
  caption: Math.round((31 * dpi) / BASE_DPI),
});

export const taskbarHeight = (scaling: Scaling): number => ({ 100: 48, 125: 60, 150: 72 })[scaling];

export const workArea = (scaling: Scaling): Rect => ({ L: 0, T: 0, R: SCREEN.w, B: SCREEN.h - taskbarHeight(scaling) });

export function clientPhysical(outerW: number, outerH: number, dpi: number): { w: number; h: number } {
  const { border, caption } = frameMetrics(dpi);
  return { w: outerW - 2 * border, h: outerH - caption - border };
}

/** The CSS viewport WebView2 would give the page: client physical px * 96 / dpi. */
export function cssViewport(outerW: number, outerH: number, dpi: number): { w: number; h: number } {
  const c = clientPhysical(outerW, outerH, dpi);
  return { w: Math.round((c.w * BASE_DPI) / dpi), h: Math.round((c.h * BASE_DPI) / dpi) };
}

export type Edge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw';

/** Resizes from an edge or corner by (dx, dy), keeping the opposite edge fixed once the minimum is hit. */
export function resizeRect(start: Placement, edge: Edge, dx: number, dy: number, minW: number, minH: number): Placement {
  let { x, y, w, h } = start;
  if (edge.includes('e')) w = Math.max(minW, start.w + dx);
  if (edge.includes('s')) h = Math.max(minH, start.h + dy);
  if (edge.includes('w')) {
    w = Math.max(minW, start.w - dx);
    x = start.x + start.w - w;
  }
  if (edge.includes('n')) {
    h = Math.max(minH, start.h - dy);
    y = start.y + start.h - h;
  }
  return { x, y, w, h };
}
