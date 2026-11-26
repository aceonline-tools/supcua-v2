import { describe, expect, it } from 'vitest';
import { canCreate, filterGroups, resolveGroup } from './group';

const G = ['Buffs', 'Farming', 'Utility', 'Buffs Extra'];

describe('group helpers', () => {
  it('filters case-insensitively by substring', () => {
    expect(filterGroups(G, 'UFF')).toEqual(['Buffs', 'Buffs Extra']);
    expect(filterGroups(G, 'ing')).toEqual(['Farming']);
    expect(filterGroups(G, '')).toEqual(G);
  });
  it('ranks an exact match first', () => expect(filterGroups(['Buffs Extra', 'Buffs'], 'buffs')[0]).toBe('Buffs'));
  it('resolves trimmed, case-insensitive matches to the existing spelling', () => {
    expect(resolveGroup('  buffs ', G)).toBe('Buffs');
    expect(resolveGroup('  New One ', G)).toBe('New One');
    expect(resolveGroup('   ', G)).toBe('');
  });
  it('offers Create only for non-empty text with no case-insensitive exact match', () => {
    expect(canCreate('buffs', G)).toBe(false);
    expect(canCreate(' Raiders ', G)).toBe(true);
    expect(canCreate('  ', G)).toBe(false);
  });
});
