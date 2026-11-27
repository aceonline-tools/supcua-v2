import { ApiError, type Transport } from './client';
import notLicensed from './fixtures/notLicensedMethods.json';
import { defaultSeed, type MockSeed } from './mock';

// main.go also installs the diagnostics RPCs (app.mounted, app.log) on the not-licensed host.
export const NOT_LICENSED_ALLOWED: readonly string[] = [...notLicensed, 'app.mounted', 'app.log'];

// Mirrors the host in not-licensed mode: serves only the allow-list, everything else is UNKNOWN_METHOD.
export class StrictNotLicensedBackend implements Transport {
  readonly calls: { method: string; params: unknown }[] = [];
  private readonly seed: MockSeed;

  constructor(seed: MockSeed = defaultSeed()) {
    this.seed = structuredClone(seed);
    this.seed.boot.mode = 'notLicensed';
  }

  subscribe(): void {}

  async call(method: string, params: unknown): Promise<unknown> {
    this.calls.push({ method, params });
    if (!NOT_LICENSED_ALLOWED.includes(method)) throw new ApiError({ code: 'UNKNOWN_METHOD', message: 'Unknown method' });
    return method === 'app.getBootState' ? this.seed.boot : {};
  }
}
