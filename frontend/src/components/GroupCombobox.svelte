<script lang="ts">
  import { canCreate, filterGroups, resolveGroup } from '../lib/group';
  let { id, value = $bindable(), options, onchange }: { id: string; value?: string | undefined; options: string[]; onchange?: () => void } = $props();

  let text = $state('');
  let open = $state(false);
  let active = $state(-1);
  let listEl = $state<HTMLElement>();

  $effect(() => { text = value ?? ''; });

  const hits = $derived(filterGroups(options, text));
  const create = $derived(canCreate(text, options));
  const items = $derived(create ? [...hits, `Create "${text.trim()}"`] : hits);
  const listId = $derived(`${id}-list`);

  function commit(v: string) {
    const next = resolveGroup(v, options);
    text = next;
    if (next !== (value ?? '')) { value = next; onchange?.(); }
  }
  function pick(i: number) {
    commit(i >= 0 && i < hits.length ? hits[i] : text);
    open = false;
  }
  function oninput() {
    open = true;
    active = text.trim() ? 0 : -1;
  }
  function scrollActive() {
    queueMicrotask(() => listEl?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' }));
  }
  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open) { open = true; active = text.trim() ? 0 : -1; }
      if (items.length) active = e.key === 'ArrowDown' ? Math.min(items.length - 1, active + 1) : Math.max(0, active - 1);
      scrollActive();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      pick(open ? active : -1);
    } else if (e.key === 'Escape' && open) {
      e.preventDefault();
      e.stopPropagation();
      open = false;
      text = value ?? '';
    }
  }
  function onblur() {
    if (!open && text === (value ?? '')) return;
    commit(text);
    open = false;
  }
</script>

<div class="combo">
  <input {id} class="input full" role="combobox" aria-expanded={open} aria-controls={listId} aria-autocomplete="list" autocomplete="off"
    aria-activedescendant={open && active >= 0 && active < items.length ? `${id}-opt-${active}` : undefined}
    bind:value={text} {oninput} {onkeydown} {onblur} onfocus={() => { open = false; }} />
  {#if open && items.length}
    <ul class="combo-list" id={listId} role="listbox" aria-label="Groups" bind:this={listEl} onmousedown={(e) => e.preventDefault()}>
      {#each items as item, i (i)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <li id="{id}-opt-{i}" role="option" aria-selected={i === active} class:create={i >= hits.length} class:active={i === active}
          onclick={() => pick(i >= hits.length ? -1 : i)} onmousemove={() => (active = i)}>{item}</li>
      {/each}
    </ul>
  {/if}
</div>
