import type { Transport } from './client';
import { ApiError } from './client';

// Browser simulator only: the page's Transport talks to the simulator shell (window.parent), which owns the one shared mock.
export function createSimTransport(): Transport {
  let nextId = 1;
  let emit: (name: string, payload: unknown) => void = () => {};
  const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: unknown) => void }>();
  window.addEventListener('message', (e: MessageEvent) => {
    if (e.source !== window.parent) return;
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
        window.parent.postMessage({ supcua: 'call', id, method, params }, '*');
      });
    },
    subscribe(h) { emit = h; },
  };
}
