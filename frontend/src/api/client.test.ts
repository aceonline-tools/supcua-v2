import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, BRIDGE_WAIT_MS, createDefaultClient } from './client';
import { createAppClient } from './bootstrap';

const posted: string[] = [];
beforeEach(() => {
  posted.length = 0;
  (window as any).chrome = { webview: { postMessage: (m: string) => posted.push(m) } };
});
afterEach(() => {
  delete (window as any).chrome;
  delete window.__resolve;
  delete window.__emit;
});

describe('webview transport', () => {
  it('posts {id, method, params} and resolves the matching reply', async () => {
    const c = createDefaultClient();
    const p = c.call('state.get');
    expect(JSON.parse(posted[0])).toEqual({ id: 1, method: 'state.get', params: {} });
    window.__resolve!({ id: 1, ok: true, result: { ok: 1 } });
    await expect(p).resolves.toEqual({ ok: 1 });
  });

  it('ignores malformed frames without consuming the pending request', async () => {
    const c = createDefaultClient();
    const p = c.call('state.get');
    for (const bad of ['{not json', 'null', '"str"', { id: 'x', ok: true }, { id: 1 }, { id: 1, ok: false }, { id: 1, ok: false, error: { code: 1 } }, null] as any[]) window.__resolve!(bad);
    window.__resolve!(JSON.stringify({ id: 1, ok: true, result: 7 }));
    await expect(p).resolves.toBe(7);
  });

  it('rejects with ApiError for an error envelope', async () => {
    const c = createDefaultClient();
    const p = c.call('state.get');
    window.__resolve!({ id: 1, ok: false, error: { code: 'NOT_FOUND', message: 'Not found' } });
    await expect(p).rejects.toMatchObject({ code: 'NOT_FOUND' });
  });

  it('isolates throwing event handlers and bad event names', () => {
    const c = createDefaultClient();
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const seen = vi.fn();
    c.on('capture.key', () => { throw new Error('boom'); });
    c.on('capture.key', seen);
    expect(() => window.__emit!('capture.key', { capture_id: 1, key: 'a' })).not.toThrow();
    expect(() => window.__emit!(undefined as any, 1)).not.toThrow();
    expect(seen).toHaveBeenCalledWith({ capture_id: 1, key: 'a' });
    err.mockRestore();
  });
});

describe('production client without a bridge', () => {
  beforeEach(() => { delete (window as any).chrome; delete (window as any).__mock; });
  afterEach(() => { vi.useRealTimers(); vi.unstubAllEnvs(); });

  it('rejects with a disconnected error and never creates a mock', async () => {
    vi.useFakeTimers();
    const c = createDefaultClient();
    const p = c.call('state.get').catch((e) => e);
    await vi.advanceTimersByTimeAsync(BRIDGE_WAIT_MS + 200);
    const e = await p;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.message).toMatch(/Disconnected/);
    expect((window as any).__mock).toBeUndefined();
  });

  it('bootstrap in a production build does not select the mock', async () => {
    vi.stubEnv('DEV', false);
    vi.stubEnv('PROD', true);
    vi.useFakeTimers();
    const c = await createAppClient();
    const p = c.call('state.get').catch((e) => e);
    await vi.advanceTimersByTimeAsync(BRIDGE_WAIT_MS + 200);
    expect(await p).toBeInstanceOf(ApiError);
    expect((window as any).__mock).toBeUndefined();
  });

  it('bootstrap in dev without a bridge uses the mock and exposes __mock', async () => {
    const c = await createAppClient();
    expect((window as any).__mock).toBeDefined();
    await expect(c.call('state.get')).resolves.toBeTruthy();
  });

  it('a bridge that appears late connects', async () => {
    vi.useFakeTimers();
    const posted: string[] = [];
    const c = createDefaultClient();
    const p = c.call('state.get');
    await vi.advanceTimersByTimeAsync(500);
    (window as any).chrome = { webview: { postMessage: (m: string) => posted.push(m) } };
    await vi.advanceTimersByTimeAsync(200);
    expect(JSON.parse(posted[0]).method).toBe('state.get');
    window.__resolve!({ id: 1, ok: true, result: 5 });
    await expect(p).resolves.toBe(5);
  });
});
