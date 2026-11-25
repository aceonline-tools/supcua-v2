<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  let { store }: { store: AppStore } = $props();
  const onScripts = $derived(store.route.name === 'scripts' || store.route.name === 'editor');
</script>

<aside class="sidebar">
  <div class="brand">supcua</div>
  <div class="subtitle">Game macro tools</div>
  <nav>
    <button class="nav" class:active={onScripts} aria-current={onScripts ? 'page' : undefined} onclick={() => store.go({ name: 'scripts' })}>▤ &nbsp; Scripts</button>
    <button class="nav" class:active={store.route.name === 'settings'} aria-current={store.route.name === 'settings' ? 'page' : undefined} onclick={() => store.go({ name: 'settings' })}>⚙ &nbsp; Settings</button>
  </nav>
  <div class="spacer"></div>
  <footer>
    Global toggle &nbsp; <kbd>{store.settings?.global_hotkey ?? ''}</kbd><br />
    supcua-v2 · {store.boot?.version ?? ''}
  </footer>
</aside>
