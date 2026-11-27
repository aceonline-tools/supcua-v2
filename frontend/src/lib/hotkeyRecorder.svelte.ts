import type { Client } from '../api/client';
import type { SettingsHotkeyField } from '../api/types';
import type { SettingResult } from './store.svelte';

export const RECORDING_TEXT = 'Press a key… (Esc to cancel)';
// Short text for the field box (one ellipsized line), the full sentence for its tooltip.
export const MSGS = {
  invalid: { short: 'Invalid or already used', full: 'That hotkey is invalid or already used by another hotkey. The previous hotkey is kept.' },
  register: { short: 'In use by another app', full: 'Could not register that hotkey; another app may be using it. The previous hotkey is kept.' },
  restore: { short: 'Could not restore old hotkey', full: 'Could not register the new hotkey and could not restore the previous one. Restart the app.' },
  busy: { short: 'Cannot record now', full: 'Cannot record right now.' },
};
export type RecorderMessage = { short: string; full: string };

/**
 * Records one Settings hotkey through the host (settings.capture.*). The host mutes every global
 * hotkey for the duration; this class always ends the capture before saving, so they are restored
 * on success, cancel, blur, error and teardown alike. The result is saved through the normal
 * settings path (same validation and registration); a refusal keeps the old value and sets `error`.
 */
export class HotkeyRecorder {
  recording = $state(false);
  error = $state<RecorderMessage | null>(null);
  private id: number | null = null;
  private busy = false;
  private starting = false;
  private cancelWhileStarting = false;
  private offs: (() => void)[];
  private get api(): Client { return this.getApi(); }

  constructor(
    private getApi: () => Client,
    private field: () => SettingsHotkeyField,
    private save: (display: string) => Promise<SettingResult>,
  ) {
    this.offs = [
      getApi().on('settings.capture.hotkey', (e) => void this.onHotkey(e)),
      getApi().on('settings.capture.ended', (e) => { if (e.capture_id === this.id) this.reset(); }),
    ];
  }

  private reset() { this.id = null; this.recording = false; }

  async start(): Promise<void> {
    if (this.recording || this.busy) return;
    this.error = null;
    this.busy = this.starting = true;
    this.cancelWhileStarting = false;
    this.recording = true;
    try {
      const { capture_id } = await this.api.call('settings.capture.start', { field: this.field() });
      if (this.cancelWhileStarting) {
        // cancelled (blur, Cancel, unmount) while the start was in flight: stop the capture that was just opened
        await this.api.call('settings.capture.stop', { capture_id }).catch(() => {});
      } else {
        this.id = capture_id;
      }
    } catch {
      this.recording = false;
      if (!this.cancelWhileStarting) this.error = MSGS.busy;
    } finally {
      this.cancelWhileStarting = false;
      this.busy = this.starting = false;
    }
  }

  /** Cancel: the value is left unchanged. Safe while the start RPC is still pending. */
  async cancel(): Promise<void> {
    if (this.starting) this.cancelWhileStarting = true;
    const id = this.id;
    this.reset();
    if (id !== null) await this.api.call('settings.capture.stop', { capture_id: id }).catch(() => {});
  }

  private async onHotkey(e: { capture_id: number; field: SettingsHotkeyField; hotkey: { modifiers: string[]; key: string } | null; display: string }) {
    if (e.capture_id !== this.id || e.field !== this.field() || this.busy) return;
    this.busy = true;
    try {
      const bareEscape = e.hotkey !== null && e.hotkey.key === 'escape' && e.hotkey.modifiers.length === 0;
      await this.cancel(); // restores the muted hotkeys before anything is rebound
      if (bareEscape || !e.display) return;
      const res = await this.save(e.display);
      if (!res.ok) {
        this.error = res.code === 'HOTKEY_RESTORE_FAILED' ? MSGS.restore : res.code === 'HOTKEY_REGISTER_FAILED' ? MSGS.register : MSGS.invalid;
      }
    } finally {
      this.busy = false;
    }
  }

  dispose(): void {
    this.offs.forEach((off) => off());
    void this.cancel();
  }
}
