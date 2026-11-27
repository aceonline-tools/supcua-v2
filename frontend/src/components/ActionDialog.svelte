<script lang="ts">
  import Icon from './Icon.svelte';
  import type { EditorStore } from '../lib/editor.svelte';
  import KeySequenceInput from './KeySequenceInput.svelte';
  import type { Action } from '../api/types';
  import { DEFAULT_DELAY_MS, sequenceOf, toKeyPressPayload, validateSequence, type KeyPressAction, type KeyStroke } from '../lib/keySequence';
  let { ed }: { ed: EditorStore } = $props();
  const LABELS: Record<string, string> = {
    x: 'X:', y: 'Y:', key: 'Key:', ms: 'Delay (ms):', count: 'Count (0=infinite):', timeout_ms: 'Timeout (ms):', use_found_position: 'Use found position:',
  };
  const d = $derived(ed.actionDialog);
  const isKeyPress = $derived(d?.action.type === 'key_press');
  const kp = $derived(d?.action as KeyPressAction | undefined);
  let delayText = $state('');
  let delayFor: object | null = null;
  $effect(() => {
    if (d && isKeyPress && delayFor !== d) { delayFor = d; delayText = String((d.action as KeyPressAction).key_delay_ms ?? DEFAULT_DELAY_MS); }
  });
  const keyErrors = $derived(isKeyPress && kp ? validateSequence(sequenceOf(kp), delayText.trim() === '' ? NaN : Number(delayText)) : {});
  const setKeys = (k: KeyStroke[]) => { if (kp) { kp.keys = k; delete kp.key; } };
  function submit() {
    if (!d) return;
    if (ed.capturing) void ed.cancelCapture();
    const snap = $state.snapshot(d.action) as KeyPressAction;
    const action = isKeyPress ? toKeyPressPayload(snap, sequenceOf(snap), Number(delayText)) : snap;
    void ed.applyActionDialog(action as Action);
  }
  const rec = (a: Action) => a as unknown as Record<string, unknown>;
  const invalid = $derived(
    !d ? false : isKeyPress ? !!keyErrors.keys || !!keyErrors.delay : d.spec.params.some((p) => {
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
      onsubmit={(e) => { e.preventDefault(); if (!invalid) submit(); }}>
      <div class="dialog-title" id="ad-title">Edit: {d.action.type}</div>
      <div class="dialog-body">
        {#if isKeyPress && kp}
          <KeySequenceInput keys={sequenceOf(kp)} delay={delayText} errors={keyErrors} recording={ed.capturing === 'keys'} note={ed.recordNote}
            onkeys={setKeys} ondelay={(v) => (delayText = v)} onrecord={() => ed.startKeyRecording()} onstop={() => ed.cancelCapture()} />
        {:else}
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
        {/if}
      </div>
      <div class="dialog-footer">
        <button type="submit" class="primary" disabled={invalid}><Icon name="check" />OK</button>
        <button type="button" onclick={() => ed.closeActionDialog()}><Icon name="x" />Cancel</button>
      </div>
    </form>
    </div>
  </div>
{/if}
