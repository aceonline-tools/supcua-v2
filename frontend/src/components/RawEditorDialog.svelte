<script lang="ts">
  import type { EditorStore } from '../lib/editor.svelte';
  let { ed }: { ed: EditorStore } = $props();
</script>

{#if ed.raw}
  <div class="modal-back" role="presentation">
    <div class="dialog wide" role="dialog" aria-modal="true" aria-labelledby="raw-title">
      <div class="dialog-title" id="raw-title">Raw JSON</div>
      <div class="dialog-body">
        <textarea class="input full raw" spellcheck="false" aria-label="Script JSON" bind:value={ed.raw.text} oninput={() => ed.raw && (ed.raw.error = '')}></textarea>
        {#if ed.raw.error}<div class="err" role="alert">{ed.raw.error}</div>{/if}
      </div>
      <div class="dialog-footer">
        <button class="primary" onclick={() => ed.raw && ed.applyRaw(ed.raw.text)}>Apply</button>
        <button onclick={() => ed.closeRaw()}>Cancel</button>
      </div>
    </div>
  </div>
{/if}
