export type LoaderStage = 'starting' | 'interface' | 'scripts';

export interface LoaderOptions {
  doc?: Document;
  now?: () => number;
  reducedMotion?: () => boolean;
  statusDelayMs?: number;
  fadeMs?: number;
  fastMs?: number;
  slowMs?: number;
}

export interface Loader {
  stage(s: LoaderStage): void;
  done(): void;
  fail(message: string): void;
}

const TEXT: Record<LoaderStage, string> = { starting: 'Starting…', interface: 'Loading interface…', scripts: 'Loading scripts…' };
const SLOW = 'Still loading… this is taking longer than usual';

// Drives the inline #boot screen from index.html. `now` is time since navigation start (performance.now),
// so the thresholds count from the moment the page began loading, not from when this bundle ran.
// - the status line becomes visible at statusDelayMs (CSS reveals it at the same time before the bundle runs)
// - done() removes the screen at once if total time < fastMs (no flicker) or under reduced motion,
//   otherwise fades it out over fadeMs; there is no minimum display time
// - slowMs: the status switches to a calm "still loading" line; the app is never revealed before readiness
// - fail(msg): themed error state with a Continue button; the app (and its error banner) is revealed only on Continue
// - #app is inert + aria-busy until the screen starts to leave, so Tab cannot reach hidden controls
export function createLoader(opts: LoaderOptions = {}): Loader {
  const doc = opts.doc ?? document;
  const now = opts.now ?? (() => performance.now());
  const reduced = opts.reducedMotion ?? (() => !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);
  const statusDelay = opts.statusDelayMs ?? 700;
  const fadeMs = opts.fadeMs ?? 150;
  const fastMs = opts.fastMs ?? 100;
  const slowMs = opts.slowMs ?? 10000;
  const root = doc.getElementById('boot');
  const app = doc.getElementById('app');
  const release = () => { app?.removeAttribute('inert'); app?.removeAttribute('aria-busy'); };
  if (!root) { release(); return { stage() {}, done() {}, fail() {} }; }
  const st = doc.getElementById('boot-st');
  const go = doc.getElementById('boot-go');
  let current: LoaderStage = 'starting';
  let slow = false;
  let finished = false;
  let failed = false;
  const timers: ReturnType<typeof setTimeout>[] = [];

  const render = () => { if (st && !failed) st.textContent = slow ? SLOW : TEXT[current]; };
  timers.push(setTimeout(() => { root.dataset.status = 'visible'; }, Math.max(0, statusDelay - now())));
  timers.push(setTimeout(() => { slow = true; root.dataset.status = 'visible'; render(); }, Math.max(0, slowMs - now())));

  const remove = () => { timers.splice(0).forEach(clearTimeout); root.remove(); };
  const focusApp = () => {
    if (!app) return;
    const heading = app.querySelector<HTMLElement>('h1,h2,[role=heading]');
    const target = heading ?? app.querySelector<HTMLElement>('button,a[href],input,select,textarea,[tabindex]:not([tabindex="-1"])') ?? app;
    if (target === heading || target === app) target.setAttribute('tabindex', '-1');
    target.focus();
  };
  const leave = () => {
    finished = true;
    timers.splice(0).forEach(clearTimeout);
    const hadFocus = root.contains(doc.activeElement);
    release();
    if (hadFocus) focusApp();
    if (now() < fastMs || reduced()) return root.remove();
    root.classList.add('out');
    timers.push(setTimeout(remove, fadeMs));
  };
  return {
    stage(s) { if (finished) return; current = s; render(); },
    done() { if (!finished && !failed) leave(); },
    fail(message) {
      if (finished || failed) return;
      failed = true;
      timers.splice(0).forEach(clearTimeout);
      root.dataset.status = 'visible';
      root.classList.add('err');
      if (st) st.textContent = message;
      if (go) { go.hidden = false; go.addEventListener('click', () => { failed = false; leave(); }, { once: true }); go.focus(); }
    },
  };
}

export type FirstLoads = { settled(): Promise<string | null>; notLicensed(): boolean };

// Wraps a client to watch the FIRST call of each named method, so the hand-off can wait until every initial
// load has actually settled (the store resolves init() as soon as any of them rejects). Reads nothing from the store.
export function trackFirstCalls<T extends { call: (...a: never[]) => Promise<unknown> }>(client: T, methods: string[]): { client: T; loads: FirstLoads } {
  const first = new Map<string, Promise<unknown>>();
  let notLicensed = false;
  const call = client.call.bind(client);
  const wrapped = {
    ...client,
    call: (...a: never[]) => {
      const p = call(...a);
      const m = a[0] as unknown as string;
      if (methods.includes(m) && !first.has(m)) {
        first.set(m, p);
        if (m === 'app.getBootState') void p.then((v) => { notLicensed = (v as { mode?: string } | undefined)?.mode === 'notLicensed'; }, () => {});
      }
      return p;
    },
  } as T;
  return {
    client: wrapped,
    loads: {
      notLicensed: () => notLicensed,
      async settled() {
        const rs = await Promise.allSettled([...first.values()]);
        const bad = rs.find((r): r is PromiseRejectedResult => r.status === 'rejected');
        if (!bad) return null;
        const r = bad.reason;
        return (r && (r.message || String(r))) || 'Failed to load';
      },
    },
  };
}

// Dev-only: `?loaderDelay=<ms>` slows every host call so the loading screen can be inspected.
export function delayCalls<T extends { call: (...a: never[]) => Promise<unknown> }>(client: T, ms: number): T {
  if (!(ms > 0)) return client;
  const call = client.call.bind(client);
  return { ...client, call: (...a: never[]) => new Promise((r) => setTimeout(r, ms)).then(() => call(...a)) } as T;
}

// Dev-only: `?loaderFail=<method>` makes that host call reject, to inspect the loading screen's error state.
export function failCall<T extends { call: (...a: never[]) => Promise<unknown> }>(client: T, method: string | null): T {
  if (!method) return client;
  const call = client.call.bind(client);
  return { ...client, call: (...a: never[]) => ((a[0] as unknown as string) === method ? Promise.reject(new Error(`${method} failed (simulated)`)) : call(...a)) } as T;
}

// Tells the host (startup log) when the loading screen was left, so the startup log and CI can wait on it. Diagnostics only.
export function reportLoader(client: { call: (...a: never[]) => Promise<unknown> }, outcome: 'done' | 'error'): void {
  try {
    const call = client.call as unknown as (m: string, p: unknown) => Promise<unknown>;
    void call.call(client, 'app.log', { level: 'info', message: `loader ${outcome} at ${Math.round(performance.now())}ms` }).catch(() => {});
  } catch {
    // diagnostics must never break the app
  }
}
