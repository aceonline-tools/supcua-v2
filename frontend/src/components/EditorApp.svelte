<script lang="ts">
  import Icon from './Icon.svelte';
  import EditorScreen from './EditorScreen.svelte';
  import type { AppStore } from '../lib/store.svelte';
  let { store }: { store: AppStore } = $props();
</script>

{#if store.editor.script}
  <EditorScreen {store} />
{:else if !store.error}
  <p class="muted editor-loading">Loading…</p>
{/if}
{#if store.error}
  <div class="notices" role="region" aria-label="Notifications">
    <div class="banner" role="alert">{store.error}<button onclick={() => (store.error = '')}><Icon name="x" />Dismiss</button></div>
  </div>
{/if}
