// Geometry scan for clipped text and unreachable controls (i18n contract 7.3). Browser-side: the
// Playwright harness injects/imports it and reports the violations; the pure helpers are unit-tested.

export interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}
export interface Violation {
  kind: 'clipped-text' | 'text-outside-clip' | 'diacritic-clip' | 'unreachable' | 'covered' | 'zero-size' | 'page-scroll-x';
  selector: string;
  detail: string;
}
export interface Metrics {
  scrollWidth: number;
  clientWidth: number;
  scrollHeight: number;
  clientHeight: number;
  overflowX: string;
  overflowY: string;
}

const CLIPPING = new Set(['hidden', 'clip']);
export const DIACRITIC_SAMPLE = 'Tiếng Việt ệ ỹ ặ Ữ Ộ';

/** Text is clipped when a hidden/clipping box is smaller than its content (1px tolerance for rounding). */
export function isClipped(m: Metrics, tolerance = 1): boolean {
  return (CLIPPING.has(m.overflowX) && m.scrollWidth > m.clientWidth + tolerance) || (CLIPPING.has(m.overflowY) && m.scrollHeight > m.clientHeight + tolerance);
}

export function outside(inner: Box, outer: Box, tolerance = 1): boolean {
  return inner.left < outer.left - tolerance || inner.top < outer.top - tolerance || inner.right > outer.right + tolerance || inner.bottom > outer.bottom + tolerance;
}

export function intersects(a: Box, b: Box): boolean {
  return a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
}

export function describe(el: Element): string {
  const id = el.id ? `#${el.id}` : '';
  const cls = typeof el.className === 'string' && el.className ? `.${el.className.trim().split(/\s+/).slice(0, 2).join('.')}` : '';
  const label = el.getAttribute('aria-label');
  return `${el.tagName.toLowerCase()}${id}${cls}${label ? `[aria-label="${label}"]` : ''}`;
}

const CONTROLS = 'button, a[href], input, select, textarea, [role=button], [role=tab], [role=switch], [role=radio], [role=combobox], [tabindex]:not([tabindex="-1"])';

const box = (r: DOMRect): Box => ({ left: r.left, top: r.top, right: r.right, bottom: r.bottom });

/** Scrolls every scrollable ancestor chain to the element, so reachability is judged after scrolling. */
function reachable(el: Element, win: Window): { ok: boolean; why: string } {
  (el as HTMLElement).scrollIntoView?.({ block: 'center', inline: 'center' });
  const r = el.getBoundingClientRect();
  if (r.width === 0 || r.height === 0) return { ok: false, why: 'zero-size' };
  const c = { x: r.left + r.width / 2, y: r.top + r.height / 2 };
  if (c.x < 0 || c.y < 0 || c.x > win.innerWidth || c.y > win.innerHeight) return { ok: false, why: 'unreachable' };
  const top = win.document.elementFromPoint(c.x, c.y);
  if (!top || (top !== el && !el.contains(top) && !top.contains(el))) return { ok: false, why: 'covered' };
  return { ok: true, why: '' };
}

/**
 * Scans the current document. Run it per route, per language, per viewport; compare the English
 * result with the Vietnamese one and count only the violations that appear in Vietnamese.
 */
export function scanLayout(win: Window = window): Violation[] {
  const doc = win.document;
  const out: Violation[] = [];
  const root = doc.scrollingElement;
  if (root && root.scrollWidth > win.innerWidth + 1) out.push({ kind: 'page-scroll-x', selector: 'document', detail: `${root.scrollWidth} > ${win.innerWidth}` });

  for (const el of Array.from(doc.body.querySelectorAll<HTMLElement>('*'))) {
    if (el.hasAttribute('data-ellipsis') || !el.childNodes.length) continue;
    const hasText = Array.from(el.childNodes).some((n) => n.nodeType === 3 && /\S/.test(n.textContent ?? ''));
    if (!hasText) continue;
    const cs = win.getComputedStyle(el);
    const m: Metrics = { scrollWidth: el.scrollWidth, clientWidth: el.clientWidth, scrollHeight: el.scrollHeight, clientHeight: el.clientHeight, overflowX: cs.overflowX, overflowY: cs.overflowY };
    if (isClipped(m)) out.push({ kind: 'clipped-text', selector: describe(el), detail: `${m.scrollWidth}x${m.scrollHeight} in ${m.clientWidth}x${m.clientHeight}` });
    const range = doc.createRange();
    range.selectNodeContents(el);
    const tr = range.getBoundingClientRect?.() ?? el.getBoundingClientRect();
    for (let p: HTMLElement | null = el; p; p = p.parentElement) {
      const pcs = win.getComputedStyle(p);
      if (!CLIPPING.has(pcs.overflowX) && !CLIPPING.has(pcs.overflowY)) continue;
      if (tr.width > 0 && outside(box(tr), box(p.getBoundingClientRect()), 1.5)) {
        out.push({ kind: 'text-outside-clip', selector: describe(el), detail: `clipped by ${describe(p)}` });
        break;
      }
    }
    if (CLIPPING.has(cs.overflowY) || CLIPPING.has(cs.overflow)) {
      const lh = parseFloat(cs.lineHeight);
      const fs = parseFloat(cs.fontSize);
      if (Number.isFinite(lh) && Number.isFinite(fs) && lh < fs * 1.3 && /[À-ỹ]/.test(el.textContent ?? '')) {
        out.push({ kind: 'diacritic-clip', selector: describe(el), detail: `line-height ${lh}px < 1.3 x ${fs}px with overflow clipping` });
      }
    }
  }
  for (const el of Array.from(doc.body.querySelectorAll<HTMLElement>(CONTROLS))) {
    if ((el as HTMLButtonElement).disabled || el.closest('[inert],[hidden]')) continue;
    const r = reachable(el, win);
    if (!r.ok) out.push({ kind: r.why as Violation['kind'], selector: describe(el), detail: r.why });
  }
  return out;
}

/** A violation counts only if the English run at the same viewport did not already have it. */
export function newInVietnamese(en: Violation[], vi: Violation[]): Violation[] {
  const seen = new Set(en.map((v) => `${v.kind}|${v.selector}`));
  return vi.filter((v) => !seen.has(`${v.kind}|${v.selector}`));
}
