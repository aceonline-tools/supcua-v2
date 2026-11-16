import type { ApiErrorBody, EventName, Events, MethodName, Methods } from './types';
import { MockBackend } from './mock';

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
    listeners.get(name)?.forEach((h) => h(payload as never));
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

function createWebViewTransport(host: WebViewHost): Transport {
  let nextId = 1;
  const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: unknown) => void; timer: ReturnType<typeof setTimeout> }>();
  let emit: (name: string, payload: unknown) => void = () => {};

  window.__resolve = (raw) => {
    const msg: ResolveMessage = typeof raw === 'string' ? JSON.parse(raw) : raw;
    const entry = pending.get(msg.id);
    if (!entry) return;
    pending.delete(msg.id);
    clearTimeout(entry.timer);
    if (msg.ok) entry.resolve(msg.result);
    else entry.reject(new ApiError(msg.error ?? { code: 'INTERNAL', message: 'Internal error' }));
  };
  window.__emit = (name, payload) => emit(name, payload);

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
  return createClient(host ? createWebViewTransport(host) : new MockBackend());
}
