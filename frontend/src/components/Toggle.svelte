<script lang="ts">
  import Icon from './Icon.svelte';
  let { checked, label, onchange, disabled = false }: { checked: boolean; label: string; onchange: (v: boolean) => void; disabled?: boolean } = $props();
</script>

<button class="switch" role="switch" aria-checked={checked} aria-label={label} title={label} {disabled} onclick={() => onchange(!checked)}><Icon name={checked ? 'check' : 'x'} size={10} /></button>
