import { createDefaultClient, type Client } from './client';

// The mock exists only in dev builds: import.meta.env.DEV is folded to false in production, which drops the import.
export async function createAppClient(): Promise<Client> {
  if (import.meta.env.DEV && !window.chrome?.webview) return (await import('./dev')).createDevClient();
  return createDefaultClient();
}
