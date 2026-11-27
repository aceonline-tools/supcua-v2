// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { createSimTransport, resolveShellOrigin } from './simTransport';

describe('resolveShellOrigin', () => {
  const own = 'https://supcua-v2.vercel.app';
  it('accepts the page origin and loopback dev ports only', () => {
    expect(resolveShellOrigin(own, own)).toBe(own);
    expect(resolveShellOrigin('http://127.0.0.1:5700', 'http://127.0.0.1:5805')).toBe('http://127.0.0.1:5700');
    expect(resolveShellOrigin('http://localhost:5700', own)).toBe('http://localhost:5700');
  });
  it.each([null, '', 'https://evil.example', 'http://127.0.0.1', 'http://127.0.0.1:5700/x', 'https://127.0.0.1:5700', 'javascript:1', 'http://127.0.0.1.evil.com:5700'])('rejects %s', (v) => {
    expect(resolveShellOrigin(v, own)).toBeNull();
  });
});

describe('createSimTransport origin pinning', () => {
  it('posts to the pinned origin and ignores replies/events from other origins', async () => {
    const post = vi.fn();
    Object.defineProperty(window, 'parent', { value: { postMessage: post }, configurable: true });
    const t = createSimTransport('http://127.0.0.1:5700');
    const events: string[] = [];
    t.subscribe((n) => events.push(n));
    const call = t.call('state.get', {});
    expect(post).toHaveBeenCalledWith({ supcua: 'call', id: 1, method: 'state.get', params: {} }, 'http://127.0.0.1:5700');
    const parent = window.parent as unknown as Window;
    const send = (origin: string, data: unknown) => window.dispatchEvent(new MessageEvent('message', { data, origin, source: parent }));
    send('http://evil.example', { supcua: 'event', name: 'state', payload: {} });
    send('http://evil.example', { supcua: 'reply', id: 1, ok: true, result: 'evil' });
    expect(events).toEqual([]);
    send('http://127.0.0.1:5700', { supcua: 'reply', id: 1, ok: true, result: 'good' });
    await expect(call).resolves.toBe('good');
    send('http://127.0.0.1:5700', { supcua: 'event', name: 'state', payload: {} });
    expect(events).toEqual(['state']);
  });
});
