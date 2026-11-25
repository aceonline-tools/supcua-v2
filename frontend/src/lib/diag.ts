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

// Reports what the page actually laid out (not just that it mounted), so a blank window can be told
// apart from a page that rendered into a zero-size or invisible surface. Diagnostics only.
export function reportRendered(client: Client): void {
  const call = client.call as unknown as (method: string, params: unknown) => Promise<unknown>;
  let frames = 0;
  requestAnimationFrame(() => { frames++; });
  const send = () => {
    try {
      const b = document.body.getBoundingClientRect();
      const bg = getComputedStyle(document.body).backgroundColor;
      const msg = `rendered body=${Math.round(b.width)}x${Math.round(b.height)} inner=${innerWidth}x${innerHeight} dpr=${devicePixelRatio} elements=${document.querySelectorAll('#app *').length} visibility=${document.visibilityState} frames=${frames} bg=${bg}`;
      void call.call(client, 'app.log', { level: 'info', message: msg }).catch(() => {});
    } catch {
      // diagnostics must never break the app
    }
  };
  setTimeout(send, 1000);
}
