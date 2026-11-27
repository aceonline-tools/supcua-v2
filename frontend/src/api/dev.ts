import { createClient, type Client } from './client';
import { MockBackend, seedFromQuery } from './mock';
import { StrictNotLicensedBackend } from './strictMock';

export async function createDevClient(): Promise<Client> {
  const params = new URLSearchParams(location.search);
  if (params.get('sim') === '1' && window.parent !== window) {
    const t = await import('./simTransport');
    const shell = t.resolveShellOrigin(params.get('shell'), location.origin);
    if (shell) return createClient(t.createSimTransport(shell));
  }
  const seed = seedFromQuery(location.search);
  if (new URLSearchParams(location.search).get('mock') === 'license-strict') return createClient(new StrictNotLicensedBackend(seed));
  const mock = new MockBackend(seed);
  (window as unknown as { __mock: MockBackend }).__mock = mock;
  const q = new URLSearchParams(location.search);
  if (q.get('window') === 'editor') {
    // Dev preview of the editor window: ?window=editor&script=<id>&actions=<n> (no script = New).
    const id = q.get('script');
    if (id && Number(q.get('actions')) > 0) mock.padActions(id, Number(q.get('actions')));
    void mock.call('editor.openWindow', id ? { id } : {});
  }
  return createClient(mock);
}
