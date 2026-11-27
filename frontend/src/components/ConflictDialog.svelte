<script lang="ts">
  import Icon from './Icon.svelte';
  import { FIELD_LABEL, type EditorStore } from '../lib/editor.svelte';
  import { formatHotkey } from '../lib/format';
  let { ed }: { ed: EditorStore } = $props();
  let yes = $state<HTMLButtonElement>();
  $effect(() => { if (ed.conflicts) yes?.focus(); });
</script>

{#if ed.conflicts}
  <div class="modal-back" role="presentation">
    <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="hc-title">
      <div class="dialog-title" id="hc-title">Hotkey Conflict</div>
      <div class="dialog-body">
        {#each ed.conflicts as c (c.scriptId + c.field)}
          <p>Hotkey “{formatHotkey(c.hotkey)}” conflicts with the {FIELD_LABEL[c.field]} hotkey of script “{c.name}”.</p>
          <p class="muted">Override and remove it from “{c.name}”?</p>
        {/each}
      </div>
      <div class="dialog-footer">
        <button bind:this={yes} class="primary" onclick={() => ed.resolveConflict(true)}><Icon name="check" />Yes</button>
        <button onclick={() => ed.resolveConflict(false)}><Icon name="x" />No</button>
      </div>
    </div>
  </div>
{/if}
