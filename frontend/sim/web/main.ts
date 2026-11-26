import {
  RULES, SCALINGS, physicalDelta, SCREEN, VERSIONS, clientPhysical, cssViewport, dpiOf, frameMetrics, resizeRect, taskbarHeight, workArea,
  type Edge, type Placement, type Scaling, type VersionId,
} from '../src/sizing';

declare const __SIM_PORTS__: Record<string, number>;
declare const __SIM_STATIC__: boolean;

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const verSel = $<HTMLSelectElement>('ver');
const sclSel = $<HTMLSelectElement>('scl');
const frame = $('frame');
const screen = $('screen');
const win = $('win');
const title = $('title');
const client = $('client');
const iframe = $<HTMLIFrameElement>('app-frame');
const taskbar = $('taskbar');
const readout = $('readout');

const q = new URLSearchParams(location.search);
let version: VersionId = __SIM_STATIC__ ? 'v1.3.0' : (VERSIONS as readonly string[]).includes(q.get('v') ?? '') ? (q.get('v') as VersionId) : 'v1.3.0';
let scaling: Scaling = (SCALINGS as readonly number[]).includes(Number(q.get('s'))) ? (Number(q.get('s')) as Scaling) : 100;
let place: Placement = { x: 0, y: 0, w: 0, h: 0 };
let k = 1;

for (const v of VERSIONS) verSel.add(new Option(v, v));
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
  const tb = taskbarHeight(scaling);
  const f = dpi() / 96;
  taskbar.style.height = px(tb);
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

function render() {
  const d = dpi();
  const { border, caption } = frameMetrics(d);
  const c = clientPhysical(place.w, place.h, d);
  const css = cssViewport(place.w, place.h, d);
  Object.assign(win.style, { left: px(place.x), top: px(place.y), width: px(place.w), height: px(place.h) });
  Object.assign(title.style, { height: px(caption), paddingLeft: px(border + 4 * (d / 96)), fontSize: px(12 * (d / 96)) });
  const ico = title.querySelector<HTMLElement>('.ico')!;
  ico.style.width = ico.style.height = px(Math.round(16 * (d / 96)));
  ico.style.marginRight = px(8 * (d / 96));
  title.querySelector<HTMLElement>('.txt')!.textContent = `supcua-v2 ${label()}`;
  title.querySelectorAll<HTMLElement>('.g').forEach((g) => {
    g.style.width = px(Math.round(46 * (d / 96)));
    g.style.fontSize = px(11 * (d / 96));
  });
  Object.assign(client.style, { left: px(border), top: px(caption), width: px(c.w), height: px(c.h) });
  Object.assign(iframe.style, { width: px(css.w), height: px(css.h), transform: `scale(${d / 96})` });
  readout.innerHTML = [
    ['version', label()],
    ['scaling', `${scaling}% (${d} dpi)`],
    ['outer', `${place.w}x${place.h}`],
    ['client', `${c.w}x${c.h}`],
    ['CSS viewport', `${css.w}x${css.h}`],
    ['DPR', `${d / 96} emulated by transform (browser reports ${window.devicePixelRatio} inside the iframe)`],
    ['pos', `${place.x},${place.y}`],
  ].map(([n, v]) => `<span>${n} <b data-k="${n}">${v}</b></span>`).join('');
  $('screen').dataset.outer = `${place.w}x${place.h}`;
}

function fresh() {
  const d = dpi();
  place = RULES[version].initial(d, workArea(scaling));
  render();
}

function sync() {
  const u = new URLSearchParams(__SIM_STATIC__ ? { s: String(scaling) } : { v: version, s: String(scaling) });
  history.replaceState(null, '', `?${u}`);
}

function loadVersion() {
  iframe.src = __SIM_STATIC__ ? './app/' : `http://127.0.0.1:${__SIM_PORTS__[version]}/`;
}

function addHandles() {
  const edges: Edge[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'];
  const cursor: Record<Edge, string> = { n: 'ns', s: 'ns', e: 'ew', w: 'ew', ne: 'nesw', sw: 'nesw', nw: 'nwse', se: 'nwse' };
  for (const edge of edges) {
    const h = document.createElement('div');
    h.className = 'h';
    h.dataset.edge = edge;
    h.style.cursor = `${cursor[edge]}-resize`;
    win.append(h);
    drag(h, (start, dx, dy) => {
      const [mw, mh] = RULES[version].minSize(dpi(), workArea(scaling));
      return resizeRect(start, edge, dx, dy, mw, mh);
    });
  }
}

function layoutHandles() {
  const b = Math.max(frameMetrics(dpi()).border, 6);
  const t = 2 * b;
  const set = (edge: string, s: Partial<CSSStyleDeclaration>) => Object.assign(win.querySelector<HTMLElement>(`[data-edge="${edge}"]`)!.style, s);
  set('n', { left: px(t), right: px(t), top: '0', height: px(b / 2 + 2) });
  set('s', { left: px(t), right: px(t), bottom: '0', height: px(b) });
  set('w', { top: px(t), bottom: px(t), left: '0', width: px(b) });
  set('e', { top: px(t), bottom: px(t), right: '0', width: px(b) });
  set('nw', { left: '0', top: '0', width: px(t), height: px(t) });
  set('ne', { right: '0', top: '0', width: px(t), height: px(t) });
  set('sw', { left: '0', bottom: '0', width: px(t), height: px(t) });
  set('se', { right: '0', bottom: '0', width: px(t), height: px(t) });
}

function drag(el: HTMLElement, next: (start: Placement, dx: number, dy: number) => Placement) {
  el.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const start = { ...place };
    const ox = e.clientX;
    const oy = e.clientY;
    el.setPointerCapture(e.pointerId);
    document.body.classList.add('dragging');
    const move = (m: PointerEvent) => {
      place = next(start, physicalDelta(m.clientX - ox, k), physicalDelta(m.clientY - oy, k));
      render();
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
      el.removeEventListener('lostpointercapture', up);
      document.body.classList.remove('dragging');
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
    el.addEventListener('lostpointercapture', up);
  });
}

drag(title, (start, dx, dy) => {
  const work = workArea(scaling);
  const { caption } = frameMetrics(dpi());
  return {
    ...start,
    x: Math.round(Math.min(Math.max(start.x + dx, 120 - start.w), SCREEN.w - 120)),
    y: Math.round(Math.min(Math.max(start.y + dy, 0), work.B - caption)),
  };
});

addHandles();
verSel.addEventListener('change', () => { version = verSel.value as VersionId; sync(); loadVersion(); fresh(); });
sclSel.addEventListener('change', () => { scaling = Number(sclSel.value) as Scaling; sync(); layoutTaskbar(); layoutHandles(); fresh(); });
$('reset').addEventListener('click', fresh);
window.addEventListener('resize', fit);
fit();
layoutTaskbar();
layoutHandles();
fresh();
loadVersion();
sync();
