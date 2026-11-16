import type { Hotkey, HotkeyMode, RunMode } from '../api/types';

export const runModeLabel = (m: RunMode): string => (m === 'infinite' ? 'Infinite' : 'Once');
export const hotkeyModeLabel = (m: HotkeyMode): string => (m === 'separate' ? 'Separate' : 'Toggle');

// Parity with ui/hotkey_input.go FormatHotkey.
export function formatHotkey(h: Hotkey | null | undefined): string {
  if (!h) return '(none)';
  const mods = h.modifiers.map((m) => m.charAt(0).toUpperCase() + m.slice(1));
  return [...mods, h.key.toUpperCase()].join(' + ');
}

export function parseHotkey(text: string): Hotkey | null {
  const parts = text.split(' + ').map((p) => p.trim()).filter(Boolean);
  if (parts.length === 0 || text === '(none)') return null;
  return { modifiers: parts.slice(0, -1).map((m) => m.toLowerCase()), key: parts[parts.length - 1].toLowerCase() };
}
