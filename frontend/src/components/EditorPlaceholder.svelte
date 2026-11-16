<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  let { store, id }: { store: AppStore; id: string | null } = $props();
  const script = $derived(store.scripts.find((s) => s.id === id));
</script>

<div class="page-head">
  <div>
    <h1>{id === null ? 'New Script' : 'Edit Script'}</h1>
    <p>{script?.name ?? ''}{#if script?.group}<span class="muted"> / {script.group}</span>{/if}</p>
  </div>
  <button class="primary" onclick={() => store.go({ name: 'scripts' })}>Close</button>
</div>
<div class="card empty"><h2>Script editor</h2><p>The editor arrives in a later task.</p></div>
