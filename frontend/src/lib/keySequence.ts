// Key-sequence helpers for key_press actions (contract: model/action.go, PR #24).
import type { Action, KeyStroke } from '../api/types';

export type { KeyStroke };
export type KeyPressAction = Action;

export const MAX_KEYS = 32;
export const DEFAULT_DELAY_MS = 50;
export const MAX_DELAY_MS = 5000;
export const MOD_ORDER = ['ctrl', 'alt', 'shift', 'win'] as const;
const MODS = new Set<string>(MOD_ORDER);

// model/keys.go keyNames; any single-byte character is also valid.
export const KEY_NAMES: readonly string[] = ['ctrl', 'lctrl', 'rctrl', 'shift', 'lshift', 'rshift', 'alt', 'lalt', 'ralt', 'win', 'enter', 'return', 'tab', 'escape', 'esc', 'space', 'backspace', 'delete', 'insert', 'home', 'end', 'pageup', 'pagedown', 'up', 'down', 'left', 'right', 'f1', 'f2', 'f3', 'f4', 'f5', 'f6', 'f7', 'f8', 'f9', 'f10', 'f11', 'f12', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z'];
const KEY_SET = new Set(KEY_NAMES);

export function isKnownKey(name: string): boolean {
  return KEY_SET.has(name) || (name.length === 1 && name.charCodeAt(0) < 128);
}

// Matches model/action.go ResolveKeys: single characters keep their case, names are lowercased.
const canonKey = (k: string) => (k.length > 1 ? k.toLowerCase() : k);

export function canonMods(mods: readonly string[] | undefined): string[] {
  const set = new Set((mods ?? []).map((m) => m.toLowerCase()));
  return MOD_ORDER.filter((m) => set.has(m));
}

/** The strokes of a key_press action: `keys` wins over the legacy `key`. */
export function sequenceOf(a: KeyPressAction): KeyStroke[] {
  if (a.keys && a.keys.length > 0) return a.keys.map((k) => ({ key: k.key, mods: canonMods(k.mods) }));
  return a.key ? [{ key: a.key, mods: [] }] : [];
}

const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);
export const formatStroke = (k: KeyStroke) =>
  [...canonMods(k.mods).map(cap), k.key.length === 1 ? k.key.toUpperCase() : cap(k.key)].join('+');
export const formatSequence = (keys: readonly KeyStroke[]) => keys.map(formatStroke).join(' \u2192 ');

/** Same shape as bridge/format.go describeKeyPress. */
export function describeKeyPress(a: KeyPressAction): string {
  const parts = sequenceOf(a).map((k) => [...canonMods(k.mods), k.key].join('+'));
  return 'Key Press: ' + parts.join(' \u2192 ');
}

/** Parses "Ctrl+Shift+A" (or "ctrl++") into a stroke; null when unusable. */
export function parseStroke(text: string): KeyStroke | null {
  const t = text.trim();
  if (!t) return null;
  let tokens: string[];
  if (t.endsWith('+') && t.length > 1) tokens = [...t.slice(0, -2).split('+').filter(Boolean), '+'];
  else tokens = t.split('+').map((s) => s.trim());
  const key = canonKey(tokens[tokens.length - 1]);
  const mods = tokens.slice(0, -1).map((m) => m.toLowerCase());
  if (!key || !isKnownKey(key) || mods.some((m) => !MODS.has(m)) || new Set(mods).size !== mods.length) return null;
  return { key, mods: canonMods(mods) };
}

export interface KeyErrors { keys?: string; delay?: string }

export function validateSequence(keys: readonly KeyStroke[], delay: number): KeyErrors {
  const e: KeyErrors = {};
  if (keys.length === 0) e.keys = 'Add at least one key.';
  else if (keys.length > MAX_KEYS) e.keys = `At most ${MAX_KEYS} keys.`;
  else if (keys.some((k) => !isKnownKey(canonKey(k.key)))) e.keys = 'Unknown key name.';
  if (!Number.isInteger(delay) || delay < 0 || delay > MAX_DELAY_MS) e.delay = `Delay must be a whole number from 0 to ${MAX_DELAY_MS} ms.`;
  return e;
}

/** Wire form: a single plain key stays the legacy {type,key}; anything else is keys[] + key_delay_ms. */
export function toKeyPressPayload(base: KeyPressAction, keys: readonly KeyStroke[], delay: number): KeyPressAction {
  const { key: _k, keys: _ks, key_delay_ms: _d, ...rest } = base;
  void _k; void _ks; void _d;
  const strokes = keys.map((k) => ({ key: canonKey(k.key), mods: canonMods(k.mods) }));
  if (strokes.length === 1 && strokes[0].mods.length === 0) return { ...rest, key: strokes[0].key };
  return {
    ...rest,
    keys: strokes.map((k) => (k.mods.length ? k : { key: k.key })),
    key_delay_ms: delay,
  };
}

export interface RecordState { keys: KeyStroke[]; mods: string[] }
export interface RecordResult extends RecordState { stop: boolean; ignored?: boolean }

/**
 * Folds one host key-down into the recording. When the host reports the modifier state
 * (`flags` is an array) it is authoritative for each key and modifier key-downs are skipped.
 * Without it (older host) the modifiers pressed since the previous key apply to the next key.
 */
export function recordEvent(state: RecordState, name: string, flags?: readonly string[] | null): RecordResult {
  const n = canonKey(name);
  if (n === 'escape' || n === 'esc') return { ...state, stop: true };
  const reported = Array.isArray(flags);
  if (MODS.has(n) && reported) return { ...state, stop: false };
  if (MODS.has(n)) return { keys: state.keys, mods: canonMods([...state.mods, n]), stop: false };
  if (!isKnownKey(n)) return { ...state, stop: false, ignored: true };
  if (state.keys.length >= MAX_KEYS) return { ...state, stop: true, ignored: true };
  const keys = [...state.keys, { key: n, mods: canonMods(reported ? flags : state.mods) }];
  return { keys, mods: [], stop: keys.length >= MAX_KEYS };
}

export function moveStroke<T>(list: readonly T[], i: number, dir: -1 | 1): T[] {
  const j = i + dir;
  if (i < 0 || i >= list.length || j < 0 || j >= list.length) return [...list];
  const out = [...list];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}
