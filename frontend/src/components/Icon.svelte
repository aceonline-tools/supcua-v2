<script lang="ts">
  import { icons, type IconName } from '../lib/icons';
  let { name, size = 14, title }: { name: IconName; size?: number; title?: string } = $props();
</script>

<svg class="icon" xmlns="http://www.w3.org/2000/svg" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
  stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden={title ? undefined : 'true'}
  role={title ? 'img' : undefined} aria-label={title} focusable="false">{#if title}<title>{title}</title>{/if}{@html icons[name]}</svg>
