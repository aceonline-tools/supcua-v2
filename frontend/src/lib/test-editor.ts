import { createClient } from '../api/client';
import type { MockBackend } from '../api/mock';
import { AppStore } from './store.svelte';

/** An editor-window store attached to a session the (mock) host opened for `id` (null = New). */
export async function editorStore(mock: MockBackend, id: string | null = 's3'): Promise<AppStore> {
  await mock.call('editor.openWindow', id ? { id } : {});
  const store = new AppStore(createClient(mock), 'editor');
  await store.init();
  await store.editor.open();
  return store;
}

export const decisions = (mock: MockBackend): string[] =>
  mock.calls.filter((c) => c.method === 'editor.closeDecision').map((c) => (c.params as { decision: string }).decision);
