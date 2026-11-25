import type { Client } from '../api/client';

// Tells the host the app rendered (app.mounted): the host's initial-load deadline is disarmed by it and
// the startup log records it. Diagnostics only: never throws, and a host without the method (dev mock) is fine.
export function reportMounted(client: Client): void {
  const call = client.call as unknown as (method: string, params: unknown) => Promise<unknown>;
  try {
    void call.call(client, 'app.mounted', { route: location.pathname || '/' }).catch(() => {});
  } catch {
    // diagnostics must never break the app
  }
}
