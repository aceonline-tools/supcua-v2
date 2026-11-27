import { describe, expect, it } from 'vitest';
import {
  MAX_KEYS, describeKeyPress, formatSequence, isKnownKey, moveStroke, parseStroke, recordEvent, sequenceOf, toKeyPressPayload, validateSequence,
  type RecordState,
} from './keySequence';

const fold = (names: string[]) => {
  let s: RecordState = { keys: [], mods: [] };
  let stopped = false;
  for (const n of names) {
    if (stopped) break;
    const r = recordEvent(s, n);
    s = { keys: r.keys, mods: r.mods };
    stopped = r.stop;
  }
  return { ...s, stopped };
};

describe('recording', () => {
  it('keeps order and attaches held modifiers to the next key only', () => {
    const r = fold(['ctrl', 'shift', 'a', 'b', 'alt', 'c']);
    expect(r.keys).toEqual([{ key: 'a', mods: ['ctrl', 'shift'] }, { key: 'b', mods: [] }, { key: 'c', mods: ['alt'] }]);
    expect(formatSequence(r.keys)).toBe('Ctrl+Shift+A → B → Alt+C');
  });
  it('Esc stops and is not recorded', () => {
    const r = fold(['a', 'escape', 'b']);
    expect(r.keys.map((k) => k.key)).toEqual(['a']);
    expect(r.stopped).toBe(true);
  });
  it('ignores unsupported host key names', () => {
    expect(recordEvent({ keys: [], mods: [] }, '0xBA')).toMatchObject({ ignored: true, stop: false, keys: [] });
  });
  it('stops at the maximum', () => {
    const r = fold(Array.from({ length: MAX_KEYS + 5 }, () => 'a'));
    expect(r.keys).toHaveLength(MAX_KEYS);
    expect(r.stopped).toBe(true);
  });
});

describe('typing, reordering', () => {
  it('parses typed strokes', () => {
    expect(parseStroke('Ctrl+Shift+A')).toEqual({ key: 'a', mods: ['ctrl', 'shift'] });
    expect(parseStroke('shift+ctrl+Enter')).toEqual({ key: 'enter', mods: ['ctrl', 'shift'] });
    expect(parseStroke('ctrl++')).toEqual({ key: '+', mods: ['ctrl'] });
    expect(parseStroke('nosuchkey')).toBeNull();
    expect(parseStroke('meta+a')).toBeNull();
    expect(parseStroke('ctrl+ctrl+a')).toBeNull();
    expect(parseStroke('é')).toBeNull();
    expect(isKnownKey(';')).toBe(true);
  });
  it('reorders and ignores out-of-range moves', () => {
    expect(moveStroke(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c']);
    expect(moveStroke(['a', 'b', 'c'], 0, -1)).toEqual(['a', 'b', 'c']);
    expect(moveStroke(['a', 'b', 'c'], 2, 1)).toEqual(['a', 'b', 'c']);
  });
});

describe('validation', () => {
  const one = [{ key: 'a' }];
  it('rejects empty, too long, bad delay', () => {
    expect(validateSequence([], 50).keys).toBeTruthy();
    expect(validateSequence(Array.from({ length: MAX_KEYS + 1 }, () => ({ key: 'a' })), 50).keys).toBeTruthy();
    expect(validateSequence(Array.from({ length: MAX_KEYS }, () => ({ key: 'a' })), 50)).toEqual({});
    for (const d of [-1, 5001, 1.5, NaN]) expect(validateSequence(one, d).delay, String(d)).toBeTruthy();
    for (const d of [0, 50, 5000]) expect(validateSequence(one, d)).toEqual({});
  });
});

describe('wire payload (frozen contract, PR #24)', () => {
  it('single plain key stays legacy', () => {
    expect(toKeyPressPayload({ type: 'key_press', keys: [{ key: 'a' }], key_delay_ms: 80 }, [{ key: 'a', mods: [] }], 80)).toEqual({ type: 'key_press', key: 'a' });
  });
  it('multi or modified sequences use keys + key_delay_ms and never an empty key', () => {
    const multi = toKeyPressPayload({ type: 'key_press', key: 'x' }, [{ key: 'a', mods: ['shift', 'ctrl'] }, { key: 'b' }], 80);
    expect(multi).toEqual({ type: 'key_press', keys: [{ key: 'a', mods: ['ctrl', 'shift'] }, { key: 'b' }], key_delay_ms: 80 });
    expect('key' in multi).toBe(false);
    expect(toKeyPressPayload({ type: 'key_press' }, [{ key: 'a', mods: ['ctrl'] }], 50))
      .toEqual({ type: 'key_press', keys: [{ key: 'a', mods: ['ctrl'] }], key_delay_ms: 50 });
  });
  it('loads legacy and multi-key actions; describes like Go', () => {
    expect(sequenceOf({ type: 'key_press', key: '7' })).toEqual([{ key: '7', mods: [] }]);
    const a = { type: 'key_press', keys: [{ key: 'a', mods: ['shift', 'ctrl'] }, { key: 'b' }], key: 'ignored' };
    expect(sequenceOf(a).map((k) => k.key)).toEqual(['a', 'b']);
    expect(describeKeyPress(a)).toBe('Key Press: ctrl+shift+a → b');
    expect(describeKeyPress({ type: 'key_press', key: '7' })).toBe('Key Press: 7');
  });
});
