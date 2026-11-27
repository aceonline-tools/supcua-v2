<script lang="ts">
  import Icon from './Icon.svelte';
  import type { AppStore } from '../lib/store.svelte';
  let { store }: { store: AppStore } = $props();
  const onScripts = $derived(store.route.name === 'scripts' || store.route.name === 'editor');
  const collapsed = $derived(store.sidebarCollapsed);
</script>

<aside class="sidebar" class:collapsed>
  <button class="side-toggle" aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-expanded={!collapsed}
    title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} onclick={() => store.setSidebarCollapsed(!collapsed)}><Icon name={collapsed ? 'panel-left-open' : 'panel-left-close'} size={16} /></button>
  {#if !collapsed}
    <div class="brand">supcua</div>
    <div class="subtitle">Game macro tools</div>
  {/if}
  <nav>
    <button class="nav" class:active={onScripts} aria-current={onScripts ? 'page' : undefined} aria-label="Scripts" title="Scripts" onclick={() => store.go({ name: 'scripts' })}><Icon name="list" size={16} />{#if !collapsed}Scripts{/if}</button>
    <button class="nav" class:active={store.route.name === 'settings'} aria-current={store.route.name === 'settings' ? 'page' : undefined} aria-label="Settings" title="Settings" onclick={() => store.go({ name: 'settings' })}><Icon name="settings" size={16} />{#if !collapsed}Settings{/if}</button>
  </nav>
  <div class="spacer"></div>
  {#if !collapsed}
    <footer>
      Global toggle &nbsp; <kbd>{store.settings?.global_hotkey ?? ''}</kbd><br />
      supcua-v2 · {store.boot?.version ?? ''}
    </footer>
  {/if}
</aside>
