import { createClient, type Client } from './client';
import { MockBackend, seedFromQuery } from './mock';

export function createDevClient(): Client {
  const mock = new MockBackend(seedFromQuery(location.search));
  (window as unknown as { __mock: MockBackend }).__mock = mock;
  return createClient(mock);
}
