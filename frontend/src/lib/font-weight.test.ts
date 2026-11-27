import { describe, expect, it } from 'vitest';
import css from '../app.css?raw';

describe('font weight', () => {
  it('only titles and group headers set a non-400 weight', () => {
    const decls = [...css.matchAll(/([^{}]+)\{[^{}]*?font-weight:([^;}]+)/g)].map((m) => [m[1].trim(), m[2].trim()]);
    const bold = decls.filter(([, w]) => w !== '400').map(([s]) => s).sort();
    expect(bold).toEqual(['.brand', '.dialog-title', '.group', 'h1,h2,h3']);
  });
});
