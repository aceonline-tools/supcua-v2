import { tick } from 'svelte';
import type { Client } from '../api/client';
import type { AppStore } from './store.svelte';
import { afterPaint, applyScrollbarMetrics, MeasureController, type MeasureMetrics } from './measure';

export interface EditorMetrics extends Partial<MeasureMetrics> {
  measuring: boolean;
  viewportW: number;
  viewportH: number;
  actionsListH: number;
  gapBottom: number;
}

declare global {
  interface Window {
    __editorMetrics?: () => EditorMetrics;
  }
}

// Boots the editor-only page: attach to the host-chosen session, then answer the host's measurement
// handshake. Subscriptions are made before any call so no host event is missed.
export function startEditorWindow(api: Client, store: AppStore, init: Promise<unknown>, gate: Promise<void> = Promise.resolve()): { measure: MeasureController; opened: Promise<boolean> } {
  applyScrollbarMetrics();
  const mark = (n: string) => performance.mark(`eo:${n}`);
  // windowReady, the session attach and the store's initial loads are independent: none waits for another.
  const ready = gate.then(() => api.call('editor.windowReady')).catch(() => ({})).then(() => mark('ready-sent'));
  const initDone = init.then(() => { mark('init'); });
  const opened = Promise.all([ready, initDone, gate.then(() => store.editor.open())])
    .then(async ([, , ok]) => { await tick(); mark('session'); return ok; });
  const measure = new MeasureController(api, { root: () => document.getElementById('editor-root'), ready: async () => { await opened; await tick(); }, paint: async () => { await store.editor.captureReady.catch(() => {}); await afterPaint(); } });
  api.on('editor.measureRequest', (e) => { performance.mark('eo:measure-request'); void measure.request(e.token); });
  api.on('editor.measureAbort', () => void measure.abort());
  window.__editorMetrics = () => {
    const m = measure.metrics();
    const list = document.querySelector('.action-list .list') as HTMLElement | null;
    return {
      ...m, measuring: measure.active, viewportW: innerWidth, viewportH: innerHeight,
      actionsListH: list?.clientHeight ?? 0, gapBottom: m ? m.clientH - m.lastBlockBottom : 0,
    };
  };
  return { measure, opened };
}
