<script lang="ts">
  import type { Hotkey } from '../api/types';
  import { formatHotkey } from '../lib/format';
  let { label, hotkey, capturing, onstart, oncancel, onclear }: {
    label: string; hotkey: Hotkey | null | undefined; capturing: boolean;
    onstart: () => void; oncancel: () => void; onclear: () => void;
  } = $props();
</script>

<div class="hk">
  <button type="button" class="input hk-field" class:capturing aria-label={label}
    onclick={() => (capturing ? oncancel() : onstart())} onblur={() => capturing && oncancel()}>
    {#if capturing}<span class="accent">Press keys… (Esc to cancel)</span>
    {:else if hotkey}<kbd>{formatHotkey(hotkey)}</kbd>
    {:else}<span class="muted">(none)</span>{/if}
  </button>
  {#if hotkey && !capturing}<button type="button" class="link" aria-label={`Clear ${label}`} onclick={onclear}>Clear</button>{/if}
</div>
