<script lang="ts">
  import type { SettingResult } from '../lib/store.svelte';
  let { value, label, numeric = false, placeholder = '', commit }: {
    value: string | number; label: string; numeric?: boolean; placeholder?: string;
    commit: (v: string | number) => Promise<SettingResult>;
  } = $props();
  let draft = $state<string | null>(null);
  let invalid = $state(false);
  const shown = $derived(draft ?? String(value));

  async function save() {
    if (draft === null || draft === String(value)) { draft = null; return; }
    const v = numeric ? Number(draft) : draft.trim();
    const res = await commit(v);
    invalid = !res.ok;
    if (res.ok) draft = null;
  }
</script>

<div class="field-wrap">
  <input class="input" class:invalid aria-label={label} aria-invalid={invalid} type={numeric ? 'number' : 'text'} {placeholder} value={shown}
    oninput={(e) => { draft = e.currentTarget.value; invalid = false; }}
    onblur={save} onkeydown={(e) => { if (e.key === 'Enter') e.currentTarget.blur(); else if (e.key === 'Escape') { draft = null; invalid = false; } }} />
  {#if invalid}<div class="err msg-float" role="alert">Invalid value</div>{/if}
</div>
