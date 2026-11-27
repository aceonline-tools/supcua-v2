<script lang="ts">
  import type { AppStore } from './lib/store.svelte';
  import Icon from './components/Icon.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import Scripts from './components/Scripts.svelte';
  import Settings from './components/Settings.svelte';
  import EditorScreen from './components/EditorScreen.svelte';
  import LicenseScreen from './components/LicenseScreen.svelte';
  import UpdateDialog from './components/UpdateDialog.svelte';
  import DeleteDialog from './components/DeleteDialog.svelte';

  let { store, copy }: { store: AppStore; copy: (text: string) => Promise<void> } = $props();
</script>

{#if store.boot?.mode === 'notLicensed'}
  <LicenseScreen {store} {copy} />
{:else if store.route.name === 'editor'}
  <EditorScreen {store} />
  <DeleteDialog {store} />
  <UpdateDialog {store} />
{:else}
<div class="app">
  <div class="shell" class:collapsed={store.sidebarCollapsed}>
    <Sidebar {store} />
    <main>
      {#if store.error}
        <div class="banner" role="alert">{store.error}<button onclick={() => (store.error = '')}><Icon name="x" />Dismiss</button></div>
      {/if}
      {#if !store.loaded}
        <p class="muted">Loading…</p>
      {:else if store.route.name === 'settings'}
        <Settings {store} />
      {:else}
        <Scripts {store} />
      {/if}
    </main>
  </div>
  <DeleteDialog {store} />
  <UpdateDialog {store} />
</div>
{/if}
