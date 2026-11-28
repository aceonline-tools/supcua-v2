<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  import type { ProcessRow } from '../api/types';
  import Icon from './Icon.svelte';
  import TextSetting from './TextSetting.svelte';
  let { store, value }: { store: AppStore; value: string } = $props();

  let open = $state(false);
  let phase = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
  let rows = $state<ProcessRow[]>([]);
  let truncated = $state(false);
  let filter = $state('');
  let pickFailed = $state(false);
  let root = $state<HTMLElement>();
  let filterEl = $state<HTMLInputElement>();
  let triggerEl = $state<HTMLButtonElement>();
  let seq = 0;
  let session = 0;
  let saving = false;

  const needle = $derived(filter.trim().toLowerCase());
  const shown = $derived(needle ? rows.filter((r) => r.exe.toLowerCase().includes(needle) || r.title.toLowerCase().includes(needle)) : rows);

  async function load() {
    const mine = ++seq;
    phase = 'loading';
    try {
      const res = await store.api.call('processes.list');
      if (mine !== seq) return;
      rows = Array.isArray(res.rows) ? res.rows : [];
      truncated = !!res.truncated;
      phase = 'ready';
    } catch {
      if (mine !== seq) return;
      rows = [];
      phase = 'error';
    }
  }
  function toggle() {
    if (open) { close(); return; }
    open = true;
    session++;
    filter = '';
    pickFailed = false;
    void load();
    queueMicrotask(() => filterEl?.focus());
  }
  function close(refocus = false) {
    open = false; seq++; session++;
    if (refocus) triggerEl?.focus();
  }
  async function pick(r: ProcessRow) {
    if (saving) return;
    const mine = session;
    saving = true;
    let ok = false;
    try { ok = (await store.setAutoActive({ processName: r.exe })).ok; } finally { saving = false; }
    if (mine !== session || !open) return;
    if (ok) close(true); else pickFailed = true;
  }
  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) { e.stopPropagation(); close(true); }
  }
  $effect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => { if (root && !root.contains(e.target as Node)) close(); };
    document.addEventListener('pointerdown', away, true);
    return () => document.removeEventListener('pointerdown', away, true);
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="ctl picker-ctl" bind:this={root} {onkeydown}>
  <TextSetting label="Game process name" {value} commit={(v) => store.setAutoActive({ processName: String(v) })} />
  <button type="button" class="picker-btn" bind:this={triggerEl} aria-label="Pick from running processes" aria-haspopup="dialog" aria-expanded={open} onclick={toggle}>
    <Icon name="list" size={14} />
  </button>
  {#if open}
    <div class="picker-pop" role="dialog" aria-label="Running processes">
      <div class="picker-head">
        <input class="input" bind:this={filterEl} bind:value={filter} aria-label="Filter processes" placeholder="Filter by name or title" />
        <button type="button" class="picker-btn" aria-label="Refresh process list" disabled={phase === 'loading'} onclick={load}>
          <Icon name="refresh-cw" size={14} />
        </button>
      </div>
      <div class="picker-body" role="listbox" aria-label="Processes" aria-busy={phase === 'loading'}>
        {#if phase === 'loading'}
          <p class="picker-msg" role="status">Loading…</p>
        {:else if phase === 'error'}
          <p class="picker-msg err" role="alert">Couldn't read the process list. You can still type a name.</p>
        {:else if rows.length === 0}
          <p class="picker-msg" role="status">{truncated ? 'List incomplete. Refresh or type a name.' : 'No windows found.'}</p>
        {:else if shown.length === 0}
          <p class="picker-msg" role="status">No matches.</p>
        {:else}
          {#each shown as r (r.exe + '\u0000' + r.title)}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="picker-row" role="option" tabindex="0" aria-selected={r.exe.toLowerCase() === value.toLowerCase()} title={`${r.exe} — ${r.title}`}
              onclick={() => pick(r)} onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); void pick(r); } }}>
              <span class="picker-exe">{r.exe}</span><span class="picker-title">{r.title}</span>
            </div>
          {/each}
          {#if truncated}<p class="picker-msg" role="status">List is partial. Refresh or type a name.</p>{/if}
        {/if}
        {#if pickFailed}<p class="picker-msg err" role="alert">Couldn't save that name.</p>{/if}
      </div>
    </div>
  {/if}
</div>
