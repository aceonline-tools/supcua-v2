import {
  EDITOR_FALLBACK_CSS, RULES, SCALINGS, SCREEN, VERSIONS, clientPhysical, cssViewport, dpiOf, editorBounds, editorMinSize, frameMetrics,
  hasEditorWindow, physicalDelta, resizeRect, taskbarHeight, workArea,
  type Edge, type Placement, type Scaling, type VersionId,
} from '../src/sizing';
import { decideMeasure, trustedMessage } from '../src/host';
import { MockHub } from '../../src/api/mockHub';
import { seedFromQuery } from '../../src/api/mock';
import type { Transport } from '../../src/api/client';

declare const __SIM_PORTS__: Record<string, number>;
declare const __SIM_STATIC__: boolean;

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const verSel = $<HTMLSelectElement>('ver');
const sclSel = $<HTMLSelectElement>('scl');
const frame = $('frame');
const screen = $('screen');
const taskbar = $('taskbar');
const readout = $('readout');

const q = new URLSearchParams(location.search);
const simKnobs = ['updcheck', 'upddelay', 'procs', 'procdelay'].flatMap((k) => (q.has(k) ? [`${k}=${q.get(k)}`] : [])).join('&');
const STATIC_VERSION: VersionId = 'current';
let version: VersionId = __SIM_STATIC__ ? STATIC_VERSION : (VERSIONS as readonly string[]).includes(q.get('v') ?? '') && __SIM_PORTS__[q.get('v') as string] ? (q.get('v') as VersionId) : 'current';
let scaling: Scaling = (SCALINGS as readonly number[]).includes(Number(q.get('s'))) ? (Number(q.get('s')) as Scaling) : 100;
let k = 1;

for (const v of VERSIONS) {
  if (__SIM_STATIC__ ? v !== STATIC_VERSION : !__SIM_PORTS__[v]) continue;
  verSel.add(new Option(v === 'current' ? 'current (editor window)' : v, v));
}
for (const s of SCALINGS) sclSel.add(new Option(`${s}%`, String(s)));
verSel.value = version;
if (__SIM_STATIC__) verSel.closest('label')!.style.display = 'none';
const label = () => (__SIM_STATIC__ ? 'main' : version);
sclSel.value = String(scaling);

const px = (n: number) => `${n}px`;
const dpi = () => dpiOf(scaling);

function fit() {
  const area = $('area').getBoundingClientRect();
  k = Math.min(area.width / SCREEN.w, area.height / SCREEN.h);
  frame.style.width = px(SCREEN.w * k);
  frame.style.height = px(SCREEN.h * k);
  screen.style.transform = `scale(${k})`;
}

function layoutTaskbar() {
  const f = dpi() / 96;
  taskbar.style.height = px(taskbarHeight(scaling));
  taskbar.style.fontSize = px(12 * f);
  taskbar.innerHTML = '';
  for (let i = 0; i < 6; i++) {
    const e = document.createElement('div');
    e.className = 'ico';
    e.style.width = e.style.height = px(Math.round(24 * f));
    taskbar.append(e);
  }
  const clock = document.createElement('div');
  clock.className = 'clock';
  clock.innerHTML = '12:00<br>1/1/2026';
  taskbar.append(clock);
}

const EDGES: Edge[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'];
const CURSOR: Record<Edge, string> = { n: 'ns', s: 'ns', e: 'ew', w: 'ew', ne: 'nesw', sw: 'nesw', nw: 'nwse', se: 'nwse' };

/** One simulated top-level window: chrome, resize handles, drag, and the iframe hosting a page. */
class SimWindow {
  readonly el = document.createElement('div');
  readonly iframe = document.createElement('iframe');
  readonly origin: string;
  place: Placement = { x: 0, y: 0, w: 0, h: 0 };
  private titleEl = document.createElement('div');
  private clientEl = document.createElement('div');
  private blocker = document.createElement('div');
  private text = document.createElement('div');

  constructor(readonly role: 'main' | 'editor', private caption: () => string, src: string, private minSize: () => [number, number], private onGesture: () => void = () => {}) {
    this.el.className = 'win';
    this.el.dataset.role = role;
    this.titleEl.className = 'title';
    const ico = document.createElement('div');
    ico.className = 'ico';
    this.text.className = 'txt';
    this.titleEl.append(ico, this.text);
    for (const g of ['–', '□', '✕']) {
      const b = document.createElement('div');
      b.className = 'g';
      b.textContent = g;
      this.titleEl.append(b);
    }
    this.clientEl.className = 'client';
    this.iframe.title = role === 'main' ? 'supcua UI' : 'supcua editor';
    this.iframe.dataset.role = role;
    this.blocker.className = 'blocker';
    this.blocker.addEventListener('mousedown', (e) => { e.preventDefault(); editor?.focus(); });
    this.clientEl.append(this.iframe);
    this.el.append(this.titleEl, this.clientEl, this.blocker);
    for (const edge of EDGES) {
      const h = document.createElement('div');
      h.className = 'h';
      h.dataset.edge = edge;
      h.style.cursor = `${CURSOR[edge]}-resize`;
      this.el.append(h);
      this.drag(h, (start, dx, dy) => {
        const [mw, mh] = this.minSize();
        return resizeRect(start, edge, dx, dy, mw, mh);
      });
    }
    this.drag(this.titleEl, (start, dx, dy) => {
      const work = workArea(scaling);
      return {
        ...start,
        x: Math.round(Math.min(Math.max(start.x + dx, 120 - start.w), SCREEN.w - 120)),
        y: Math.round(Math.min(Math.max(start.y + dy, 0), work.B - frameMetrics(dpi()).caption)),
      };
    });
    screen.insertBefore(this.el, taskbar);
    this.origin = src.startsWith('http') ? new URL(src).origin : location.origin;
    this.iframe.src = src;
  }

  get closeButton(): HTMLElement {
    return this.titleEl.querySelector('.g:last-child') as HTMLElement;
  }

  setBlocked(on: boolean) {
    this.el.dataset.blocked = String(on);
    this.el.classList.toggle('inactive', on);
    for (const part of [this.titleEl, this.clientEl]) {
      part.inert = on;
      part.toggleAttribute('aria-hidden', on);
      if (on) part.setAttribute('aria-hidden', 'true');
    }
    if (on) this.iframe.blur();
  }

  show() {
    this.el.style.visibility = 'visible';
    this.el.dataset.state = 'open';
  }

  focus() {
    this.iframe.focus();
    this.iframe.contentWindow?.focus();
  }

  hide() {
    this.el.style.visibility = 'hidden';
    this.el.dataset.state = 'measuring';
  }

  destroy() {
    this.el.remove();
  }

  render() {
    const d = dpi();
    const { border, caption } = frameMetrics(d);
    const c = clientPhysical(this.place.w, this.place.h, d);
    const css = cssViewport(this.place.w, this.place.h, d);
    Object.assign(this.el.style, { left: px(this.place.x), top: px(this.place.y), width: px(this.place.w), height: px(this.place.h) });
    Object.assign(this.titleEl.style, { height: px(caption), paddingLeft: px(border + 4 * (d / 96)), fontSize: px(12 * (d / 96)) });
    const ico = this.titleEl.querySelector<HTMLElement>('.ico')!;
    ico.style.width = ico.style.height = px(Math.round(16 * (d / 96)));
    ico.style.marginRight = px(8 * (d / 96));
    this.text.textContent = this.caption();
    this.titleEl.querySelectorAll<HTMLElement>('.g').forEach((g) => {
      g.style.width = px(Math.round(46 * (d / 96)));
      g.style.fontSize = px(11 * (d / 96));
    });
    Object.assign(this.clientEl.style, { left: px(border), top: px(caption), width: px(c.w), height: px(c.h) });
    Object.assign(this.iframe.style, { width: px(css.w), height: px(css.h), transform: `scale(${d / 96})` });
    this.el.dataset.outer = `${this.place.w}x${this.place.h}`;
    this.el.dataset.css = `${css.w}x${css.h}`;
    const b = Math.max(border, 6);
    const t = 2 * b;
    const set = (edge: string, s: Partial<CSSStyleDeclaration>) => Object.assign(this.el.querySelector<HTMLElement>(`[data-edge="${edge}"]`)!.style, s);
    set('n', { left: px(t), right: px(t), top: '0', height: px(b / 2 + 2) });
    set('s', { left: px(t), right: px(t), bottom: '0', height: px(b) });
    set('w', { top: px(t), bottom: px(t), left: '0', width: px(b) });
    set('e', { top: px(t), bottom: px(t), right: '0', width: px(b) });
    set('nw', { left: '0', top: '0', width: px(t), height: px(t) });
    set('ne', { right: '0', top: '0', width: px(t), height: px(t) });
    set('sw', { left: '0', bottom: '0', width: px(t), height: px(t) });
    set('se', { right: '0', bottom: '0', width: px(t), height: px(t) });
  }

  private drag(el: HTMLElement, next: (start: Placement, dx: number, dy: number) => Placement) {
    el.addEventListener('pointerdown', (e) => {
      if (e.button !== 0 || (e.target as HTMLElement).closest('.g')) return;
      e.preventDefault();
      const start = { ...this.place };
      const ox = e.clientX;
      const oy = e.clientY;
      el.setPointerCapture(e.pointerId);
      document.body.classList.add('dragging');
      const move = (m: PointerEvent) => {
        this.place = next(start, physicalDelta(m.clientX - ox, k), physicalDelta(m.clientY - oy, k));
        this.render();
        renderReadout();
      };
      const up = () => {
        el.removeEventListener('pointermove', move);
        el.removeEventListener('pointerup', up);
        el.removeEventListener('pointercancel', up);
        el.removeEventListener('lostpointercapture', up);
        document.body.classList.remove('dragging');
        this.onGesture();
      };
      el.addEventListener('pointermove', move);
      el.addEventListener('pointerup', up);
      el.addEventListener('pointercancel', up);
      el.addEventListener('lostpointercapture', up);
    });
  }
}

// ---- session state -------------------------------------------------------------------------------------------------

let main: SimWindow;
let editor: SimWindow | null = null;
let hub: MockHub | null = null;
const channels = new Map<Window, Transport>();
const remembered = new Map<Scaling, Placement>(); // the editor's bounds, kept for the sim session
let measure = { token: 1, retries: 0, cssW: 0, cssH: 0, timer: 0 };

const appUrl = (extra: string) => (__SIM_STATIC__ ? `./app/index.html?${extra}` : `http://127.0.0.1:${__SIM_PORTS__[version]}/?${extra}`);
const simUrl = (extra: string) => appUrl(`${extra}&sim=1&shell=${encodeURIComponent(location.origin)}`);
const mainCenter = () => ({ cx: main.place.x + Math.trunc(main.place.w / 2), cy: main.place.y + Math.trunc(main.place.h / 2) });

function renderReadout() {
  const d = dpi();
  const css = cssViewport(main.place.w, main.place.h, d);
  const c = clientPhysical(main.place.w, main.place.h, d);
  const rows: [string, string][] = [
    ['version', label()],
    ['scaling', `${scaling}% (${d} dpi)`],
    ['outer', `${main.place.w}x${main.place.h}`],
    ['client', `${c.w}x${c.h}`],
    ['CSS viewport', `${css.w}x${css.h}`],
    ['DPR', `${d / 96} emulated by transform (browser reports ${window.devicePixelRatio} inside the iframe)`],
    ['pos', `${main.place.x},${main.place.y}`],
  ];
  if (editor) {
    const ec = cssViewport(editor.place.w, editor.place.h, d);
    rows.push(['editor outer', `${editor.place.w}x${editor.place.h}`], ['editor CSS', `${ec.w}x${ec.h}`], ['editor pos', `${editor.place.x},${editor.place.y}`]);
  }
  readout.innerHTML = rows.map(([n, v]) => `<span>${n} <b data-k="${n}">${v}</b></span>`).join('');
}

function renderAll() {
  main.render();
  editor?.render();
  renderReadout();
}

// ---- the editor window host: creation, measure handshake, close --------------------------------------------------

function rememberEditor() {
  if (editor) remembered.set(scaling, { ...editor.place });
}

function showEditor(win: SimWindow, cssW: number, cssH: number, prior: Placement | null) {
  const b = editorBounds(cssW, cssH, dpi(), workArea(scaling), mainCenter(), prior);
  win.place = b.place;
  win.render();
  win.show();
  win.focus();
  window.clearTimeout(measure.timer);
  renderReadout();
}

function createEditorWindow(gen: number, id: string | null) {
  editor?.destroy();
  measure = { token: 1, retries: 0, cssW: 0, cssH: 0, timer: 0 };
  const caption = () => `${id ? 'Edit Script' : 'New Script'} — supcua-v2 ${label()}`;
  const win = new SimWindow('editor', caption, simUrl(`window=editor&gen=${gen}`), () => editorMinSize(dpi(), workArea(scaling)), rememberEditor);
  editor = win;
  win.hide();
  const own = remembered.get(scaling) ?? null;
  win.place = editorBounds(EDITOR_FALLBACK_CSS.w, EDITOR_FALLBACK_CSS.h, dpi(), workArea(scaling), mainCenter(), own).place;
  win.render();
  main.setBlocked(true);
  win.closeButton.addEventListener('click', () => hub?.hostCloseRequest());
  // The host's own fallback: no measurement in time → show at the fallback size and tell the page to leave measure mode.
  measure.timer = window.setTimeout(() => {
    if (editor !== win || win.el.dataset.state === 'open') return;
    hub?.backend.push('editor.measureAbort', { reason: 'timeout' });
    showEditor(win, EDITOR_FALLBACK_CSS.w, EDITOR_FALLBACK_CSS.h, own);
  }, 12000);
  renderReadout();
}

function destroyEditorWindow() {
  window.clearTimeout(measure.timer);
  rememberEditor();
  if (editor) channels.delete(editor.iframe.contentWindow as Window);
  editor?.destroy();
  editor = null;
  main.setBlocked(false);
  main.focus();
  renderReadout();
}

/** The host's answer to editor.measured (contract §5): convert with the sim's DPI, clamp, resize, then show. */
function onMeasured(win: SimWindow, p: { token: number; cssW: number; cssH: number }): unknown {
  const own = remembered.get(scaling) ?? null;
  const bounds = () => editorBounds(p.cssW, p.cssH, dpi(), workArea(scaling), mainCenter(), own);
  const d = decideMeasure(measure, p, bounds);
  switch (d.kind) {
    case 'stale': return { next: 'remeasure', token: d.token, cssW: d.cssW };
    case 'fallback':
      showEditor(win, EDITOR_FALLBACK_CSS.w, EDITOR_FALLBACK_CSS.h, own);
      return { next: 'show' };
    case 'remeasure':
      measure.retries++;
      measure.token = d.token;
      win.place = bounds().place;
      win.render();
      return { next: 'remeasure', token: d.token, cssW: d.cssW };
    default:
      measure.cssW = p.cssW;
      measure.cssH = p.cssH;
      showEditor(win, p.cssW, p.cssH, own);
      return { next: 'show' };
  }
}

window.addEventListener('message', (e: MessageEvent) => {
  const m = e.data as { supcua?: string; id?: number; method?: string; params?: unknown; down?: boolean; code?: string; key?: string; ctrl?: boolean; alt?: boolean; shift?: boolean };
  if (m?.supcua === 'key' && hub && e.source === main.iframe.contentWindow && trustedMessage(e, { source: main.iframe.contentWindow, origin: main.origin })) {
    hub.backend.hostKey(String(m.code), !!m.down, { ctrl: !!m.ctrl, alt: !!m.alt, shift: !!m.shift }, String(m.key ?? ''));
    return;
  }
  if (m?.supcua !== 'call' || typeof m.method !== 'string' || !e.source) return;
  const src = e.source as Window;
  const win = src === main.iframe.contentWindow ? main : editor && src === editor.iframe.contentWindow ? editor : null;
  if (!win || !hub || !trustedMessage(e, { source: win.iframe.contentWindow, origin: win.origin })) return;
  let ch = channels.get(src);
  if (!ch) {
    const gen = win.role === 'editor' ? hub.editorGen : 0;
    ch = hub.transport(win.role, gen);
    ch.subscribe((name, payload) => win.iframe.contentWindow?.postMessage({ supcua: 'event', name, payload }, win.origin));
    channels.set(src, ch);
  }
  const reply = (body: object) => src.postMessage({ supcua: 'reply', id: m.id, ...body }, win.origin);
  ch.call(m.method, m.params).then(
    (result) => {
      if (win.role === 'editor' && m.method === 'editor.measured') result = onMeasured(win, m.params as never);
      reply({ ok: true, result });
    },
    (err: { code?: string; message?: string }) => reply({ ok: false, error: { code: err.code ?? 'INTERNAL', message: err.message ?? 'Internal error' } }),
  );
});

// ---- versions, scaling, reset ---------------------------------------------------------------------------------

function newHub() {
  if (editor) destroyEditorWindow();
  channels.clear();
  hub = hasEditorWindow(version) ? new MockHub({ createEditorWindow, destroyEditorWindow: () => destroyEditorWindow() }, seedFromQuery(simKnobs)) : null;
}

function loadMain() {
  main?.destroy();
  const legacy = !hasEditorWindow(version);
  main = new SimWindow('main', () => `supcua-v2 ${label()}`, legacy ? appUrl('') : simUrl('window=main'), () => RULES[version].minSize(dpi(), workArea(scaling)));
  newHub();
  fresh();
}

function fresh() {
  main.place = RULES[version].initial(dpi(), workArea(scaling));
  remembered.clear();
  renderAll();
}

function sync() {
  const u = new URLSearchParams(__SIM_STATIC__ ? { s: String(scaling) } : { v: version, s: String(scaling) });
  history.replaceState(null, '', `?${u}`);
}

function rescale() {
  // WM_DPICHANGED: the main window is re-placed as a fresh launch; an open editor keeps its measured CSS size.
  main.place = RULES[version].initial(dpi(), workArea(scaling));
  if (editor) {
    const b = editorBounds(measure.cssW || EDITOR_FALLBACK_CSS.w, measure.cssH || EDITOR_FALLBACK_CSS.h, dpi(), workArea(scaling), mainCenter(), null);
    editor.place = b.place;
  }
  renderAll();
}

verSel.addEventListener('change', () => { version = verSel.value as VersionId; sync(); loadMain(); });
sclSel.addEventListener('change', () => { scaling = Number(sclSel.value) as Scaling; sync(); layoutTaskbar(); rescale(); });
$('reset').addEventListener('click', () => { if (editor) rescale(); else fresh(); });
window.addEventListener('resize', fit);
fit();
layoutTaskbar();
loadMain();
sync();
