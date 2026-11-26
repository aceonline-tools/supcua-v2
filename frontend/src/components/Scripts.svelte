<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  import { runModeLabel } from '../lib/format';
  import Toggle from './Toggle.svelte';
  let { store }: { store: AppStore } = $props();

  const active = $derived(store.state?.global_active ?? false);
  const sub = $derived(
    !active ? 'Inactive' : store.state?.running_name ? `Running: ${store.state.running_name}` : 'Ready for script hotkeys',
  );
  const lock = (v: boolean) => (v ? 'locked' : 'unlocked');
  const failed = $derived(Object.keys(store.unsaved).length);
</script>

<div class="page-head dense">
  <div><h1>Scripts</h1><p>Your routines, one hotkey away.</p></div>
  <button class="primary" onclick={() => store.openNew()}>+ New</button>
</div>

<div class="card status">
  <span class="dot" class:on={active}></span>
  <div><strong>Global Active</strong><div class="muted hint">{sub}</div></div>
  <Toggle checked={active} label="Global Active" onchange={(v) => store.setGlobalActive(v)} />
  <span class="spacer"></span>
  <span class="muted">Mouse: {lock(store.state?.mouse_locked ?? false)}</span>
  <span class="muted">Keyboard: {lock(store.state?.keyboard_locked ?? false)}</span>
  <kbd>{store.settings?.global_hotkey ?? ''}</kbd>
</div>

<div class="toolbar">
  <h2>Script library</h2>
  <span class="spacer"></span>
  <button disabled={!store.selected} onclick={() => store.openEditor()}>Edit</button>
  <button disabled={!store.selected} onclick={() => store.cloneSelected()}>Clone script</button>
  <button disabled={!store.selected} onclick={() => store.requestDelete()}>Delete</button>
</div>

{#if failed > 0}
  <div class="banner" role="alert">{failed} {failed === 1 ? 'change' : 'changes'} not saved to disk.</div>
{/if}

{#if store.scripts.length === 0}
  <div class="card empty">
    <h2>No scripts yet</h2>
    <p>Create your first script to bind actions to a hotkey.</p>
    <button class="primary" onclick={() => store.openNew()}>+ New</button>
  </div>
{:else}
  <div class="card table" role="table" aria-label="Script library">
    <div class="tr th" role="row"><span></span><span>Name</span><span>Hotkey</span><span>Mode</span></div>
    {#each store.rows as row (row.kind === 'group' ? 'g:' + row.group.name : row.script.id)}
      {#if row.kind === 'group'}
        <div class="tr group" role="row">
          <span>
            <input class="cb" type="checkbox" aria-label={`Enable all in ${row.group.name}`}
              checked={row.checked} indeterminate={row.indeterminate}
              onchange={() => store.setGroupEnabled(row.group.name, !row.checked)} />
          </span>
          <span>
            <button class="chev" aria-expanded={!row.collapsed} aria-label={`${row.collapsed ? 'Expand' : 'Collapse'} ${row.group.name}`}
              onclick={() => store.toggleCollapsed(row.group.name)}>{row.collapsed ? '▸' : '▾'}</button>{row.group.name}<small>{row.group.count} {row.group.count === 1 ? 'script' : 'scripts'}</small>
          </span>
          <span></span><span></span>
        </div>
      {:else}
        {@const sc = row.script}
        <div class="tr row" class:selected={store.selectedId === sc.id} role="row" tabindex="0"
          onclick={() => store.select(sc.id)} ondblclick={() => store.openEditor(sc.id)}
          onkeydown={(e) => { if (e.target !== e.currentTarget) return; if (e.key === 'Enter') store.openEditor(sc.id); else if (e.key === ' ') { e.preventDefault(); store.select(sc.id); } }}>
          <span><input class="cb" type="checkbox" aria-label={`Enable ${sc.name}`} checked={sc.enabled}
            onclick={(e) => e.stopPropagation()} onchange={(e) => store.setEnabled(sc.id, e.currentTarget.checked)} /></span>
          <span class="name" class:tree={row.grouped}>
            {sc.name}
            {#if store.unsaved[sc.id]}
              <span class="badge-unsaved" title={store.unsaved[sc.id]}>unsaved</span>
              <button class="link" onclick={(e) => { e.stopPropagation(); store.retrySave(sc.id); }}>Retry</button>
            {/if}
          </span>
          <span><kbd>{sc.hotkey_label}</kbd></span>
          <span class="muted">{runModeLabel(sc.run_mode)}</span>
        </div>
      {/if}
    {/each}
  </div>
  <div class="table-foot">
    <span>{store.scripts.length} {store.scripts.length === 1 ? 'script' : 'scripts'} · {store.enabledCount} enabled</span>
    <span class="spacer"></span>
    {#if store.selected}<span>Selected: {store.selected.name}</span>{/if}
    <span>Double-click to edit</span>
  </div>
{/if}
