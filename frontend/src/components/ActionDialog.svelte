<script lang="ts">
  import Icon from './Icon.svelte';
  import type { EditorStore } from '../lib/editor.svelte';
  import type { Action } from '../api/types';
  let { ed }: { ed: EditorStore } = $props();
  const LABELS: Record<string, string> = {
    x: 'X:', y: 'Y:', key: 'Key:', ms: 'Delay (ms):', count: 'Count (0=infinite):', timeout_ms: 'Timeout (ms):', use_found_position: 'Use found position:',
  };
  const d = $derived(ed.actionDialog);
  const rec = (a: Action) => a as unknown as Record<string, unknown>;
  const invalid = $derived(
    !!d && d.spec.params.some((p) => {
      if (p.kind !== 'int') return false;
      const v = Number(rec(d.action)[p.name] ?? 0);
      return !Number.isInteger(v) || (p.min !== undefined && v < p.min) || (p.max !== undefined && v > p.max);
    }),
  );
  let first = $state<HTMLElement>();
  $effect(() => { if (d) first?.focus(); });
</script>

{#if d}
  <div class="modal-back" role="presentation">
    <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="ad-title">
    <form
      onsubmit={(e) => { e.preventDefault(); if (!invalid) void ed.applyActionDialog($state.snapshot(d.action) as Action); }}>
      <div class="dialog-title" id="ad-title">Edit: {d.action.type}</div>
      <div class="dialog-body">
        {#each d.spec.params as p, i (p.name)}
          <div class="field"><label for={'ap-' + p.name}>{LABELS[p.name] ?? p.name}</label>
            {#if p.kind === 'bool'}
              <label class="hit"><input id={'ap-' + p.name} class="cb" type="checkbox" checked={!!rec(d.action)[p.name]}
                onchange={(e) => (rec(d.action)[p.name] = e.currentTarget.checked)} /></label>
            {:else if p.name === 'key'}
              <button type="button" id={'ap-' + p.name} class="input hk-field full" class:capturing={ed.capturing === 'key'}
                bind:this={first} onclick={() => ed.startKeyCapture()}><Icon name="keyboard" />
                {#if ed.capturing === 'key'}<span class="accent">Press a key…</span>{:else}{d.action.key ?? ''}{/if}</button>
            {:else}
              <input id={'ap-' + p.name} class="input full" type="number" min={p.min} max={p.max}
                bind:this={first} value={Number(rec(d.action)[p.name] ?? 0)}
                oninput={(e) => (rec(d.action)[p.name] = e.currentTarget.valueAsNumber)} />
            {/if}
          </div>
        {/each}
      </div>
      <div class="dialog-footer">
        <button type="submit" class="primary" disabled={invalid}><Icon name="check" />OK</button>
        <button type="button" onclick={() => ed.closeActionDialog()}><Icon name="x" />Cancel</button>
      </div>
    </form>
    </div>
  </div>
{/if}
