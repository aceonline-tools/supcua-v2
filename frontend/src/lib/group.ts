const key = (s: string) => s.trim().toLowerCase();

export function filterGroups(options: readonly string[], text: string): string[] {
  const q = key(text);
  const hits = options.filter((o) => key(o).includes(q));
  const exact = hits.findIndex((o) => key(o) === q);
  if (q && exact > 0) hits.unshift(...hits.splice(exact, 1));
  return hits;
}

export function resolveGroup(text: string, options: readonly string[]): string {
  const t = text.trim();
  return options.find((o) => key(o) === key(t)) ?? t;
}

export function canCreate(text: string, options: readonly string[]): boolean {
  const t = text.trim();
  return t !== '' && !options.some((o) => key(o) === key(t));
}
