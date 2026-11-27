import diagInit from '../public/diag-init.js?raw';
import { beforeAll, describe, expect, it, vi } from 'vitest';

const posted: { id: number; method: string; params: { level: string; message: string } }[] = [];

beforeAll(() => {
  (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: (m: string) => posted.push(JSON.parse(m)) } };
  new Function(diagInit)();
});

describe('diag-init.js (classic script, runs before the module)', () => {
  it('forwards window errors to app.log', () => {
    window.dispatchEvent(new ErrorEvent('error', { message: 'boom', filename: 'a.js', lineno: 3, colno: 7 }));
    expect(posted.at(-1)).toMatchObject({ method: 'app.log', params: { level: 'error', message: 'boom at a.js:3:7' } });
    expect(posted.at(-1)!.id).toBeGreaterThanOrEqual(8000000);
  });

  it('forwards a failed script/module load (error on the element, capture phase)', () => {
    const s = document.createElement('script');
    s.src = 'https://app.invalid/assets/index-abc.js';
    document.head.appendChild(s);
    s.dispatchEvent(new Event('error'));
    expect(posted.at(-1)!.params.message).toBe('resource failed to load: https://app.invalid/assets/index-abc.js');
  });

  it('forwards unhandled promise rejections', () => {
    const ev = new Event('unhandledrejection') as Event & { reason?: unknown };
    ev.reason = new Error('top-level await failed');
    window.dispatchEvent(ev);
    expect(posted.at(-1)!.params.message).toContain('unhandled rejection: ');
    expect(posted.at(-1)!.params.message).toContain('top-level await failed');
  });

  it('bounds message length and total count', () => {
    window.dispatchEvent(new ErrorEvent('error', { message: 'x'.repeat(2000) }));
    expect(posted.every((p) => p.params.message.length <= 500)).toBe(true);
    for (let i = 0; i < 50; i++) window.dispatchEvent(new ErrorEvent('error', { message: 'again' }));
    expect(posted.length).toBe(20);
  });

  it('is a no-op without the native bridge', () => {
    vi.stubGlobal('chrome', undefined);
    expect(() => window.dispatchEvent(new ErrorEvent('error', { message: 'dev' }))).not.toThrow();
    vi.unstubAllGlobals();
  });
});
