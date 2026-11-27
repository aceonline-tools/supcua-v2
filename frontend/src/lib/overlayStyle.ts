import type { OverlayStyleDTO } from '../api/types';

// Mirrors model/overlay_style.go. The defaults reproduce the original overlay exactly.
export const OVERLAY_DEFAULTS: OverlayStyleDTO = {
  radius: 0, background: '#404040', text_color: '', background_opacity: 1, opacity: 200 / 255,
  width: 136, height: 32, x: 10, y: 10,
};
export const OVERLAY_LIMITS = { minWidth: 80, maxWidth: 1024, minHeight: 24, maxHeight: 256, minOpacity: 0.2, minPos: -32768, maxPos: 32767 };
export const STATUS_TEXT_FALLBACK = '#00FF00';

const HEX = /^#[0-9a-f]{6}$/i;
export const isHex = (v: string): boolean => HEX.test(v);
export const maxRadius = (w: number, h: number): number => Math.floor(Math.min(w, h) / 2);

const clamp = (v: number, lo: number, hi: number): number => Math.min(Math.max(v, lo), hi);

/** Returns the first invalid field name, or '' when the style is acceptable (mirrors the backend). */
export function validateOverlayStyle(s: OverlayStyleDTO): string {
  const int = Number.isInteger;
  if (!isHex(s.background)) return 'background';
  if (s.text_color !== '' && !isHex(s.text_color)) return 'text_color';
  if (!(s.background_opacity >= 0 && s.background_opacity <= 1)) return 'background_opacity';
  if (!(s.opacity >= 0 && s.opacity <= 1)) return 'opacity';
  if (!int(s.radius) || s.radius < 0) return 'radius';
  if (!int(s.width) || s.width < 1 || s.width > 4096) return 'width';
  if (!int(s.height) || s.height < 1 || s.height > 4096) return 'height';
  if (!int(s.x) || s.x < OVERLAY_LIMITS.minPos || s.x > OVERLAY_LIMITS.maxPos) return 'x';
  if (!int(s.y) || s.y < OVERLAY_LIMITS.minPos || s.y > OVERLAY_LIMITS.maxPos) return 'y';
  return '';
}

/** Applies the floors the backend applies (size, radius, overall opacity). */
export function clampOverlayStyle(s: OverlayStyleDTO): OverlayStyleDTO {
  const width = clamp(s.width, OVERLAY_LIMITS.minWidth, OVERLAY_LIMITS.maxWidth);
  const height = clamp(s.height, OVERLAY_LIMITS.minHeight, OVERLAY_LIMITS.maxHeight);
  return {
    ...s, background: s.background.toUpperCase(), text_color: s.text_color.toUpperCase(), width, height,
    radius: clamp(s.radius, 0, maxRadius(width, height)),
    background_opacity: clamp(s.background_opacity, 0, 1), opacity: clamp(s.opacity, OVERLAY_LIMITS.minOpacity, 1),
    x: clamp(s.x, -1920 + 40 - width, 1920 - 40), y: clamp(s.y, -1080 + 16 - height, 1080 - 16),
  };
}

/** Validates one numeric text field of the Settings form; returns the accepted value or null. */
export function parseOverlayNumber(field: 'radius' | 'width' | 'height' | 'x' | 'y', raw: string | number, s: OverlayStyleDTO): number | null {
  if (typeof raw === 'string' && raw.trim() === '') return null;
  const v = Number(raw);
  if (!Number.isInteger(v)) return null;
  switch (field) {
    case 'radius': return v >= 0 && v <= maxRadius(s.width, s.height) ? v : null;
    case 'width': return v >= OVERLAY_LIMITS.minWidth && v <= OVERLAY_LIMITS.maxWidth ? v : null;
    case 'height': return v >= OVERLAY_LIMITS.minHeight && v <= OVERLAY_LIMITS.maxHeight ? v : null;
    default: return v >= OVERLAY_LIMITS.minPos && v <= OVERLAY_LIMITS.maxPos ? v : null;
  }
}
