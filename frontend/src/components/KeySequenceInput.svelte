<script lang="ts">
  import Icon from './Icon.svelte';
  import { KEY_NAMES, MAX_KEYS, MAX_DELAY_MS, formatStroke, formatSequence, parseStroke, type KeyErrors, type KeyStroke } from '../lib/keySequence';
  let { keys, delay, errors, recording, note, onkeys, ondelay, onrecord, onstop, first = $bindable() }: {
    first?: HTMLElement;
    keys: KeyStroke[]; delay: string; errors: KeyErrors; recording: boolean; note: string;
    onkeys: (k: KeyStroke[]) => void; ondelay: (v: string) => void; onrecord: () => void; onstop: () => void;
  } = $props();
  let typed = $state('');
  let typedBad = $state(false);
  const full = $derived(keys.length >= MAX_KEYS);

  function add() {
    const s = parseStroke(typed);
    if (!s || full) { typedBad = true; return; }
    typedBad = false;
    typed = '';
    onkeys([...keys, s]);
  }
  const remove = (i: number) => onkeys(keys.filter((_, j) => j !== i));
  function move(i: number, dir: -1 | 1) {
    const j = i + dir;
    if (j < 0 || j >= keys.length) return;
    const out = [...keys];
    [out[i], out[j]] = [out[j], out[i]];
    onkeys(out);
  }
</script>

<div class="keyseq">
  <div class="seq-preview" aria-label="Key sequence">{#if keys.length}<kbd title={formatSequence(keys)}>{formatSequence(keys)}</kbd>{:else}<span class="muted">(no keys)</span>{/if}</div>
  <ol class="chips" aria-label="Keys">
    {#each keys as k, i (i)}
      <li class="chip"><span class="num">{i + 1}</span><kbd>{formatStroke(k)}</kbd>
        <button type="button" class="link" aria-label={`Move ${formatStroke(k)} up`} disabled={i === 0} onclick={() => move(i, -1)}><Icon name="arrow-up" size={12} /></button>
        <button type="button" class="link" aria-label={`Move ${formatStroke(k)} down`} disabled={i === keys.length - 1} onclick={() => move(i, 1)}><Icon name="arrow-down" size={12} /></button>
        <button type="button" class="link" aria-label={`Remove ${formatStroke(k)}`} onclick={() => remove(i)}><Icon name="x" size={12} /></button>
      </li>
    {/each}
  </ol>
  <div class="inline">
    {#if recording}
      <button type="button" class="input hk-field capturing" aria-label="Stop recording" onclick={onstop}><Icon name="circle-stop" /><span class="accent">Recording… press keys (Esc to stop)</span></button>
    {:else}
      <button type="button" aria-label="Record keys" disabled={full} onclick={onrecord}><Icon name="keyboard" />Record</button>
    {/if}
  </div>
  <div class="inline">
    <input class="input" type="text" list="key-names" bind:this={first} placeholder="e.g. Ctrl+Shift+A" aria-label="Add key" aria-invalid={typedBad}
      bind:value={typed} oninput={() => (typedBad = false)}
      onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); add(); } }} />
    <datalist id="key-names">{#each KEY_NAMES as n (n)}<option value={n}></option>{/each}</datalist>
    <button type="button" aria-label="Add typed key" disabled={full} onclick={add}><Icon name="plus" />Add</button>
  </div>
  <div class="msg-slot two">
    {#if typedBad}<div class="error small" role="alert">Unknown key. Use a key name with optional Ctrl/Alt/Shift/Win, e.g. Ctrl+A.</div>{/if}
    {#if note}<div class="muted small" role="status">{note}</div>{/if}
    {#if errors.keys}<div class="error small" role="alert">{errors.keys}</div>{/if}
  </div>
  <div class="field"><label for="ap-key-delay">Delay between keys (ms):</label>
    <input id="ap-key-delay" class="input" type="number" min="0" max={MAX_DELAY_MS} step="1" value={delay}
      aria-invalid={!!errors.delay} oninput={(e) => ondelay(e.currentTarget.value)} /></div>
  <div class="msg-slot one">{#if errors.delay}<div class="error small" role="alert">{errors.delay}</div>{/if}</div>
  <div class="muted small count">{keys.length}/{MAX_KEYS} keys. Modifiers held before a key are recorded with it.</div>
</div>

<style>
  .keyseq { display: flex; flex-direction: column; gap: 8px; }
  .chips { display: flex; flex-wrap: wrap; align-content: flex-start; gap: 6px; list-style: none; margin: 0; padding: 0; height: 66px; overflow-y: auto; scrollbar-gutter: stable; }
  .keyseq > * { min-width: 0; }
  .count { height: 32px; overflow: hidden; }
  .seq-preview { height: 20px; overflow: hidden; }
  .seq-preview kbd { display: block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; }
  .chip { display: inline-flex; align-items: center; gap: 4px; padding: 2px 6px; border: 1px solid var(--border, #8884); border-radius: 6px; }
  .num { opacity: 0.6; font-size: 11px; }
  .error { color: var(--danger, #d33); }
</style>
