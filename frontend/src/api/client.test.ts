import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createDefaultClient } from './client';

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
    expect(() => window.__emit!('capture.key', { key: 'a' })).not.toThrow();
    expect(() => window.__emit!(undefined as any, 1)).not.toThrow();
    expect(seen).toHaveBeenCalledWith({ key: 'a' });
    err.mockRestore();
  });
});
