declare global {
  interface Window {
    __role?: string;
    __gen?: number;
  }
}

export type PageRole = 'main' | 'editor';

// The host navigates the editor window to index.html?window=editor&gen=<n> and injects window.__role/__gen;
// the query is authoritative for routing, the injected values are a fallback.
export function pageRole(search: string = location.search): PageRole {
  const w = new URLSearchParams(search).get('window');
  if (w === 'editor') return 'editor';
  return w === null && window.__role === 'editor' ? 'editor' : 'main';
}

export function pageGen(search: string = location.search): number {
  const n = Number(new URLSearchParams(search).get('gen'));
  if (Number.isSafeInteger(n) && n > 0) return n;
  return Number.isSafeInteger(window.__gen) && (window.__gen ?? 0) > 0 ? (window.__gen as number) : 0;
}
