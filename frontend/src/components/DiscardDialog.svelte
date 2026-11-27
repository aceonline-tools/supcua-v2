<script lang="ts">
  import Icon from './Icon.svelte';
  import type { EditorStore } from '../lib/editor.svelte';
  let { ed }: { ed: EditorStore } = $props();
  let keep = $state<HTMLButtonElement>();
  $effect(() => { if (ed.discardPrompt !== null) keep?.focus(); });
</script>

{#if ed.discardPrompt !== null}
  <div class="modal-back" role="presentation">
    <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="dc-title">
      <div class="dialog-title" id="dc-title">Unsaved Changes</div>
      <div class="dialog-body">
        <p>{ed.discardPrompt}</p>
        <p class="muted">Keep editing to fix it, or discard your changes and close.</p>
      </div>
      <div class="dialog-footer">
        <button bind:this={keep} class="primary" onclick={() => ed.keepEditing()}><Icon name="pencil" />Keep editing</button>
        <button onclick={() => ed.discardAndClose()}><Icon name="undo-2" />Discard changes</button>
      </div>
    </div>
  </div>
{/if}
