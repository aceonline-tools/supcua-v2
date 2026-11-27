import type { Transport } from './client';
import { ApiError } from './client';

/** The shell origin a sim page trusts: the `shell` query value, only if it is this page's own origin or a loopback dev origin. */
export function resolveShellOrigin(param: string | null, own: string): string | null {
  if (!param) return null;
  let u: URL;
  try { u = new URL(param); } catch { return null; }
  if (u.origin !== param) return null;
  if (u.origin === own) return u.origin;
  return u.protocol === 'http:' && (u.hostname === '127.0.0.1' || u.hostname === 'localhost') && u.port !== '' ? u.origin : null;
}

// Browser simulator only: the page's Transport talks to the simulator shell (window.parent), which owns the one shared mock.
export function createSimTransport(shellOrigin: string): Transport {
  let nextId = 1;
  let emit: (name: string, payload: unknown) => void = () => {};
  const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: unknown) => void }>();
  window.addEventListener('message', (e: MessageEvent) => {
    if (e.source !== window.parent || e.origin !== shellOrigin) return;
    const m = e.data as { supcua?: string; id?: number; ok?: boolean; result?: unknown; error?: ConstructorParameters<typeof ApiError>[0]; name?: string; payload?: unknown };
    if (m?.supcua === 'event' && typeof m.name === 'string') emit(m.name, m.payload);
    if (m?.supcua !== 'reply' || typeof m.id !== 'number') return;
    const p = pending.get(m.id);
    if (!p) return;
    pending.delete(m.id);
    if (m.ok) p.resolve(m.result);
    else p.reject(new ApiError(m.error ?? { code: 'INTERNAL', message: 'Internal error' }));
  });
  return {
    call(method, params) {
      const id = nextId++;
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject });
        window.parent.postMessage({ supcua: 'call', id, method, params }, shellOrigin);
      });
    },
    subscribe(h) { emit = h; },
  };
}
