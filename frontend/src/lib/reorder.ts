export function dropIndex(from: number, gap: number): number {
  return gap > from ? gap - 1 : gap;
}

export function moveItem<T>(list: readonly T[], from: number, gap: number): T[] {
  const out = list.slice();
  if (from < 0 || from >= list.length) return out;
  const to = dropIndex(from, Math.max(0, Math.min(list.length, gap)));
  if (to === from) return out;
  const [item] = out.splice(from, 1);
  out.splice(to, 0, item);
  return out;
}

export function gapAt(y: number, rects: readonly { top: number; bottom: number }[]): number {
  for (let i = 0; i < rects.length; i++) if (y < (rects[i].top + rects[i].bottom) / 2) return i;
  return rects.length;
}

export function scrollStep(y: number, top: number, bottom: number, zone = 24, max = 14): number {
  if (y < top + zone) return -Math.ceil((max * Math.min(zone, top + zone - y)) / zone);
  if (y > bottom - zone) return Math.ceil((max * Math.min(zone, y - (bottom - zone))) / zone);
  return 0;
}

export function insertIndex(selected: number, length: number): number {
  return selected >= 0 && selected < length ? selected + 1 : length;
}
