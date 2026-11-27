<script lang="ts">
  import Icon from './Icon.svelte';
  import type { AppStore } from '../lib/store.svelte';
  let { store, copy }: { store: AppStore; copy: (text: string) => Promise<void> } = $props();
  const mac = $derived(store.boot?.macs[0] ?? '');
  let copied = $state(false);
  async function onCopy() {
    try { await copy(mac); copied = true; } catch { copied = false; }
  }
</script>

<div class="center-page">
  <div class="dialog license">
    <div class="dialog-title">Not Licensed</div>
    <div class="dialog-body inline">
      <input class="input" readonly aria-label="Machine address" value={mac} />
      <button onclick={onCopy}>{#if copied}<Icon name="check" />Copied{:else}<Icon name="copy" />Copy{/if}</button>
    </div>
  </div>
</div>
