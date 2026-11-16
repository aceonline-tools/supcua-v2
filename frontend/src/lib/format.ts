import type { HotkeyMode, RunMode } from '../api/types';

export const runModeLabel = (m: RunMode): string => (m === 'infinite' ? 'Infinite' : 'Once');
export const hotkeyModeLabel = (m: HotkeyMode): string => (m === 'separate' ? 'Separate' : 'Toggle');
