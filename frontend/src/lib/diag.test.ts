import { describe, expect, it, vi } from 'vitest';
import { reportMounted } from './diag';
import type { Client } from '../api/client';

describe('reportMounted', () => {
  it('sends app.mounted with the current route', () => {
    const call = vi.fn().mockResolvedValue({});
    reportMounted({ call, on: vi.fn() } as unknown as Client);
    expect(call).toHaveBeenCalledWith('app.mounted', { route: location.pathname || '/' });
  });

  it('never throws or rejects when the host refuses the call', async () => {
    const rejecting = { call: vi.fn().mockRejectedValue(new Error('UNKNOWN_METHOD')), on: vi.fn() } as unknown as Client;
    expect(() => reportMounted(rejecting)).not.toThrow();
    const throwing = { call: () => { throw new Error('boom'); }, on: vi.fn() } as unknown as Client;
    expect(() => reportMounted(throwing)).not.toThrow();
    await Promise.resolve();
  });
});
