<script lang="ts">
  import type { AppStore } from './lib/store.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import Scripts from './components/Scripts.svelte';
  import Settings from './components/Settings.svelte';
  import EditorPlaceholder from './components/EditorPlaceholder.svelte';
  import DeleteDialog from './components/DeleteDialog.svelte';

  let { store }: { store: AppStore } = $props();
</script>

<div class="app">
  <div class="shell">
    <Sidebar {store} />
    <main>
      {#if store.error}
        <div class="banner" role="alert">{store.error}<button onclick={() => (store.error = '')}>Dismiss</button></div>
      {/if}
      {#if !store.loaded}
        <p class="muted">Loading…</p>
      {:else if store.route.name === 'settings'}
        <Settings {store} />
      {:else if store.route.name === 'editor'}
        <EditorPlaceholder {store} id={store.route.id} />
      {:else}
        <Scripts {store} />
      {/if}
    </main>
  </div>
  <DeleteDialog {store} />
</div>
