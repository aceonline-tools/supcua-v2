import { createClient, type Client } from './client';
import { MockBackend, seedFromQuery } from './mock';
import { StrictNotLicensedBackend } from './strictMock';

export function createDevClient(): Client {
  const seed = seedFromQuery(location.search);
  if (new URLSearchParams(location.search).get('mock') === 'license-strict') return createClient(new StrictNotLicensedBackend(seed));
  const mock = new MockBackend(seed);
  (window as unknown as { __mock: MockBackend }).__mock = mock;
  return createClient(mock);
}
