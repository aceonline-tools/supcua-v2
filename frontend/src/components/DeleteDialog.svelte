<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  let { store }: { store: AppStore } = $props();
  let confirmBtn = $state<HTMLButtonElement>();
  $effect(() => { if (store.pendingDelete) confirmBtn?.focus(); });
</script>

{#if store.pendingDelete}
  <div class="modal-back" role="presentation" onkeydown={(e) => e.key === 'Escape' && store.cancelDelete()}>
    <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="del-title">
      <div class="dialog-title" id="del-title">Delete script</div>
      <div class="dialog-body">Delete “{store.pendingDelete.name}”? This cannot be undone.</div>
      <div class="dialog-footer">
        <button bind:this={confirmBtn} class="primary" onclick={() => store.confirmDelete()}>Delete</button>
        <button onclick={() => store.cancelDelete()}>Cancel</button>
      </div>
    </div>
  </div>
{/if}
