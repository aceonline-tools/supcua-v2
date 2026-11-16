<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  let { store }: { store: AppStore } = $props();
  const u = $derived(store.update);
</script>

{#if u}
  <div class="modal-back" role="presentation">
    <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="up-title">
      <div class="dialog-title" id="up-title">Update Available</div>
      <div class="dialog-body">
        New version {u.tag} available.
        {#if u.applying}
          <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={u.pct}><span style:width={u.pct + '%'}></span></div>
        {/if}
        {#if u.failed}<div class="err" role="alert">{u.failed}</div>{/if}
      </div>
      <div class="dialog-footer">
        <button class="primary" disabled={u.applying} onclick={() => store.applyUpdate()}>Update</button>
        <button disabled={u.applying} onclick={() => store.skipUpdate()}>Skip</button>
      </div>
    </div>
  </div>
{/if}
