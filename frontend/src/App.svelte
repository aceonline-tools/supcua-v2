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
  const failed = $derived(Object.keys(store.unsaved).length);
  let hideUnsaved = $state(false);
  let seq = 0;
  let errSeq = $state(0);
  let unsSeq = $state(0);
  let notices = $state<HTMLElement>();
  $effect(() => { if (store.error) errSeq = ++seq; });
  $effect(() => { if (failed > 0) { hideUnsaved = false; unsSeq = ++seq; } });
  $effect(() => {
    const el = notices;
    if (!el) { document.documentElement.style.removeProperty('--notice-h'); return; }
    const root = document.documentElement;
    const size = () => root.style.setProperty('--notice-h', `${el.offsetHeight + 12}px`);
    size();
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(size);
    ro?.observe(el);
    const mo = new MutationObserver(() => { el.inert = !!document.querySelector('.modal-back'); });
    mo.observe(document.body, { childList: true, subtree: true });
    el.inert = !!document.querySelector('.modal-back');
    return () => { ro?.disconnect(); mo.disconnect(); root.style.removeProperty('--notice-h'); };
  });
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
      {#if !store.loaded}
        <p class="muted">Loading…</p>
      {:else if store.route.name === 'settings'}
        <Settings {store} />
      {:else}
        <Scripts {store} />
      {/if}
    </main>
  </div>
  {#if store.error || (failed > 0 && !hideUnsaved)}
    <div class="notices" role="region" aria-label="Notifications" bind:this={notices}>
      {#if failed > 0 && !hideUnsaved}
        <div class="banner" role="alert" style:order={unsSeq}>{failed} {failed === 1 ? 'change' : 'changes'} not saved to disk.<button onclick={() => (hideUnsaved = true)}><Icon name="x" />Dismiss</button></div>
      {/if}
      {#if store.error}
        <div class="banner" role="alert" style:order={errSeq}>{store.error}<button onclick={() => (store.error = '')}><Icon name="x" />Dismiss</button></div>
      {/if}
    </div>
  {/if}
  <DeleteDialog {store} />
  <UpdateDialog {store} />
</div>
{/if}
