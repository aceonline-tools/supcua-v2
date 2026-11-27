<script lang="ts">
  import { onDestroy } from 'svelte';
  import Icon from './Icon.svelte';
  import { HotkeyRecorder, RECORDING_TEXT } from '../lib/hotkeyRecorder.svelte';
  import type { Client } from '../api/client';
  import type { SettingsHotkeyField } from '../api/types';
  import type { SettingResult } from '../lib/store.svelte';
  let { label, value, field, api, save }: {
    label: string; value: string; field: SettingsHotkeyField; api: Client;
    save: (display: string) => Promise<SettingResult>;
  } = $props();
  const rec = new HotkeyRecorder(() => api, () => field, (d) => save(d));
  onDestroy(() => rec.dispose());
</script>

<div class="field-wrap">
  <div class="ctl hk-rec">
    <span class="input hk-rec-value" class:capturing={rec.recording} class:invalid={!!rec.error} role="status" aria-label={label} aria-live="polite">
      {#if rec.recording}<span class="accent">{RECORDING_TEXT}</span>{:else}<kbd>{value}</kbd>{/if}
    </span>
    <button type="button" class="hk-rec-btn" aria-label={`Capture ${label}`} aria-pressed={rec.recording}
      onclick={() => (rec.recording ? rec.cancel() : rec.start())} onblur={() => rec.recording && rec.cancel()}>
      <Icon name="keyboard" size={14} />Capture
    </button>
  </div>
  {#if rec.error}<div class="err msg-float" role="alert">{rec.error}</div>{/if}
</div>
