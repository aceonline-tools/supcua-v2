export type LoaderStage = 'starting' | 'interface' | 'scripts';

export interface LoaderOptions {
  doc?: Document;
  now?: () => number;
  reducedMotion?: () => boolean;
  statusDelayMs?: number;
  fadeMs?: number;
  fastMs?: number;
  failsafeMs?: number;
}

export interface Loader {
  stage(s: LoaderStage): void;
  done(): void;
}

const TEXT: Record<LoaderStage, string> = { starting: 'Starting…', interface: 'Loading interface…', scripts: 'Loading scripts…' };

// Drives the inline #boot screen from index.html. `now` is time since navigation start (performance.now),
// so the thresholds count from the moment the page began loading, not from when this bundle ran.
// - the status line becomes visible at statusDelayMs (CSS reveals it at the same time before the bundle runs)
// - done() removes the screen at once if total time < fastMs (no flicker) or under reduced motion,
//   otherwise fades it out over fadeMs; there is no minimum display time
// - failsafeMs: if done() was never called (initial load hung) the screen is removed so the UI/error banner shows
export function createLoader(opts: LoaderOptions = {}): Loader {
  const doc = opts.doc ?? document;
  const now = opts.now ?? (() => performance.now());
  const reduced = opts.reducedMotion ?? (() => !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);
  const statusDelay = opts.statusDelayMs ?? 700;
  const fadeMs = opts.fadeMs ?? 150;
  const fastMs = opts.fastMs ?? 100;
  const failsafeMs = opts.failsafeMs ?? 10000;
  const root = doc.getElementById('boot');
  if (!root) return { stage() {}, done() {} };
  const st = doc.getElementById('boot-st');
  let current: LoaderStage = 'starting';
  let finished = false;
  const timers: ReturnType<typeof setTimeout>[] = [];

  const render = () => { if (st) st.textContent = TEXT[current]; };
  timers.push(setTimeout(() => { root.dataset.status = 'visible'; }, Math.max(0, statusDelay - now())));

  const remove = () => { timers.splice(0).forEach(clearTimeout); root.remove(); };
  const done = () => {
    if (finished) return;
    finished = true;
    timers.splice(0).forEach(clearTimeout);
    if (now() < fastMs || reduced()) return root.remove();
    root.classList.add('out');
    timers.push(setTimeout(remove, fadeMs));
  };
  timers.push(setTimeout(done, Math.max(0, failsafeMs - now())));
  return {
    stage(s) { if (finished) return; current = s; render(); },
    done,
  };
}

// Dev-only: `?loaderDelay=<ms>` slows every host call so the loading screen can be inspected.
export function delayCalls<T extends { call: (...a: never[]) => Promise<unknown> }>(client: T, ms: number): T {
  if (!(ms > 0)) return client;
  const call = client.call.bind(client);
  return { ...client, call: (...a: never[]) => new Promise((r) => setTimeout(r, ms)).then(() => call(...a)) } as T;
}
