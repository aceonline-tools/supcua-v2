import type { ApiErrorBody, EventName, Events, MethodName, Methods } from './types';
import { MockBackend, seedFromQuery } from './mock';
import { validEventPayload } from './validate';

export class ApiError extends Error {
  readonly code: ApiErrorBody['code'];
  readonly body: ApiErrorBody;
  constructor(body: ApiErrorBody) {
    super(body.message);
    this.name = 'ApiError';
    this.code = body.code;
    this.body = body;
  }
}

export interface Transport {
  call(method: string, params: unknown): Promise<unknown>;
  subscribe(handler: (name: string, payload: unknown) => void): void;
}

type Handler<N extends EventName> = (payload: Events[N]) => void;

export interface Client {
  call<M extends MethodName>(method: M, ...params: Methods[M]['params'] extends undefined ? [] : [Methods[M]['params']]): Promise<Methods[M]['result']>;
  on<N extends EventName>(name: N, handler: Handler<N>): () => void;
}

export function createClient(transport: Transport): Client {
  const listeners = new Map<string, Set<(payload: never) => void>>();
  transport.subscribe((name, payload) => {
    if (!validEventPayload(name, payload)) return;
    listeners.get(name)?.forEach((h) => {
      try { h(payload as never); } catch (e) { console.error('event handler failed', name, e); }
    });
  });
  return {
    async call(method, ...params) {
      return (await transport.call(method, params[0] ?? {})) as never;
    },
    on(name, handler) {
      let set = listeners.get(name);
      if (!set) listeners.set(name, (set = new Set()));
      set.add(handler as never);
      return () => set.delete(handler as never);
    },
  };
}

interface WebViewHost {
  postMessage(message: string): void;
}

interface ResolveMessage {
  id: number;
  ok: boolean;
  result?: unknown;
  error?: ApiErrorBody;
}

declare global {
  interface Window {
    chrome?: { webview?: WebViewHost };
    __resolve?: (msg: ResolveMessage | string) => void;
    __emit?: (name: string, payload: unknown) => void;
  }
}

const CALL_TIMEOUT_MS = 30_000;

function isEnvelope(v: unknown): v is ResolveMessage {
  if (typeof v !== 'object' || v === null) return false;
  const m = v as Record<string, unknown>;
  if (!Number.isInteger(m.id) || typeof m.ok !== 'boolean') return false;
  if (m.ok) return true;
  const e = m.error as Record<string, unknown> | undefined;
  return typeof e === 'object' && e !== null && typeof e.code === 'string' && typeof e.message === 'string';
}

function createWebViewTransport(host: WebViewHost): Transport {
  let nextId = 1;
  const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: unknown) => void; timer: ReturnType<typeof setTimeout> }>();
  let emit: (name: string, payload: unknown) => void = () => {};

  window.__resolve = (raw) => {
    let parsed: unknown = raw;
    if (typeof raw === 'string') {
      try { parsed = JSON.parse(raw); } catch { return; }
    }
    if (!isEnvelope(parsed)) return;
    const msg = parsed;
    const entry = pending.get(msg.id);
    if (!entry) return;
    pending.delete(msg.id);
    clearTimeout(entry.timer);
    if (msg.ok) entry.resolve(msg.result);
    else entry.reject(new ApiError(msg.error ?? { code: 'INTERNAL', message: 'Internal error' }));
  };
  window.__emit = (name, payload) => {
    if (typeof name !== 'string') return;
    try { emit(name, payload); } catch (e) { console.error('emit failed', e); }
  };

  return {
    call(method, params) {
      const id = nextId++;
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new ApiError({ code: 'BUSY', message: 'Request timed out' }));
        }, CALL_TIMEOUT_MS);
        pending.set(id, { resolve, reject, timer });
        try {
          host.postMessage(JSON.stringify({ id, method, params }));
        } catch (e) {
          pending.delete(id);
          clearTimeout(timer);
          reject(e);
        }
      });
    },
    subscribe(handler) {
      emit = handler;
    },
  };
}

export function createDefaultClient(): Client {
  const host = typeof window !== 'undefined' ? window.chrome?.webview : undefined;
  if (host) return createClient(createWebViewTransport(host));
  const mock = new MockBackend(seedFromQuery(typeof location !== 'undefined' ? location.search : ''));
  if (typeof window !== 'undefined') (window as unknown as { __mock: MockBackend }).__mock = mock; // browser-only dev/screenshot hook
  return createClient(mock);
}
