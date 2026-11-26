<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  import type { HotkeyField } from '../api/types';
  import HotkeyInput from './HotkeyInput.svelte';
  import ActionDialog from './ActionDialog.svelte';
  import RawEditorDialog from './RawEditorDialog.svelte';
  import ConflictDialog from './ConflictDialog.svelte';
  import DiscardDialog from './DiscardDialog.svelte';
  let { store }: { store: AppStore } = $props();
  const ed = $derived(store.editor);
  const sc = $derived(ed.script);
  const num = (i: number) => String(i + 1).padStart(2, '0');
  const mouseButtons: [string, string][] = [
    ['Left Click', 'mouse_left_click'], ['Right Click', 'mouse_right_click'], ['Left Double', 'mouse_left_double'],
    ['Right Double', 'mouse_right_double'], ['Left Down', 'mouse_left_down'], ['Right Down', 'mouse_right_down'],
    ['Left Up', 'mouse_left_up'], ['Right Up', 'mouse_right_up'], ['Store Pos', 'mouse_store_pos'], ['Move Stored', 'mouse_move_stored'],
  ];
  const locks: [string, string][] = [
    ['Lock Mouse', 'lock_mouse'], ['Unlock Mouse', 'unlock_mouse'], ['Lock Keyboard', 'lock_keyboard'], ['Unlock Keyboard', 'unlock_keyboard'],
  ];
  const hk = (field: HotkeyField) => (field === 'toggle' ? sc?.toggle_hotkey : field === 'start' ? sc?.start_hotkey : sc?.stop_hotkey);

  function onkeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    if (ed.capturing || ed.capturePending()) { e.preventDefault(); void ed.cancelCapture(); }
    else if (ed.conflicts) void ed.resolveConflict(false);
    else if (ed.actionDialog) ed.closeActionDialog();
    else if (ed.raw) ed.closeRaw();
  }
</script>

<svelte:window {onkeydown} />

{#if sc}
  <main class="editor-main">
    <div class="page-head">
      <div>
        <h1>{store.route.name === 'editor' && store.route.id === null ? 'New Script' : 'Edit Script'}</h1>
        <p>{sc.name}{#if sc.group}<span class="muted">&nbsp;/ {sc.group}</span>{/if}</p>
      </div>
      <span class="indicator" class:ok={ed.indicator.saved} role="status">{ed.indicator.saved ? '✓' : '●'} {ed.indicator.text}</span>
    </div>

    <div class="editor-grid">
      <div class="card editor-card">
        <h3>Script Properties</h3>
        <div class="field"><label for="ed-name">Name:</label>
          <input id="ed-name" class="input full" bind:value={sc.name} oninput={() => ed.markDirty()} /></div>
        <div class="field"><label for="ed-group">Group:</label>
          <input id="ed-group" class="input full" bind:value={sc.group} oninput={() => ed.markDirty()} /></div>
        <div class="field"><label for="ed-enabled">Enabled:</label>
          <label class="hit"><input id="ed-enabled" class="cb" type="checkbox" bind:checked={sc.enabled} onchange={() => ed.markDirty()} /></label></div>
        <div class="field"><span class="lbl" id="rm">Run Mode:</span>
          <div class="radio-line" role="radiogroup" aria-labelledby="rm">
            <label><input type="radio" value="once" bind:group={sc.run_mode} onchange={() => ed.markDirty()} /> Once</label>
            <label><input type="radio" value="infinite" bind:group={sc.run_mode} onchange={() => ed.markDirty()} /> Infinite</label>
          </div></div>
        <div class="field"><span class="lbl" id="hm">Hotkey Mode:</span>
          <div class="radio-line" role="radiogroup" aria-labelledby="hm">
            <label><input type="radio" value="toggle" bind:group={sc.hotkey_mode} onchange={() => ed.markDirty()} /> Toggle</label>
            <label><input type="radio" value="separate" bind:group={sc.hotkey_mode} onchange={() => ed.markDirty()} /> Separate</label>
          </div></div>
        {#if sc.hotkey_mode === 'toggle'}
          <div class="field"><span class="lbl">Toggle Hotkey (click &amp; press keys)</span>
            <HotkeyInput label="Toggle hotkey" hotkey={hk('toggle')} capturing={ed.capturing === 'toggle'}
              onstart={() => ed.startCapture('toggle')} oncancel={() => ed.cancelCapture()} onclear={() => ed.clearHotkey('toggle')} /></div>
        {:else}
          <div class="field"><span class="lbl">Start / Stop Hotkeys (click &amp; press keys)</span>
            <div class="inline"><span>Start:</span>
              <HotkeyInput label="Start hotkey" hotkey={hk('start')} capturing={ed.capturing === 'start'}
                onstart={() => ed.startCapture('start')} oncancel={() => ed.cancelCapture()} onclear={() => ed.clearHotkey('start')} /></div>
            <div class="inline" style:margin-top="8px"><span>Stop:</span>
              <HotkeyInput label="Stop hotkey" hotkey={hk('stop')} capturing={ed.capturing === 'stop'}
                onstart={() => ed.startCapture('stop')} oncancel={() => ed.cancelCapture()} onclear={() => ed.clearHotkey('stop')} /></div></div>
        {/if}
        <div class="muted small">Click the hotkey field, then press your shortcut.</div>
      </div>

      <div class="card editor-card builder">
        <section>
          <h3>Mouse</h3>
          <div class="inline" style:margin-bottom="8px"><span>X: {ed.mouse.x}</span><span>Y: {ed.mouse.y}</span><span class="spacer"></span>
            <button onclick={() => ed.addMouseMove()}>Move</button></div>
          <div class="button-grid">{#each mouseButtons as [label, type] (type)}<button onclick={() => ed.addSimple(type)}>{label}</button>{/each}</div>
          <div class="muted small tight">Press {store.settings?.capture_hotkey ?? 'P'} to capture cursor position</div>
        </section>
        <section>
          <h3>Keyboard</h3>
          <div class="inline" style:margin-bottom="6px">
            <button type="button" class="input hk-field" class:capturing={ed.capturing === 'key'} aria-label="Key"
              onclick={() => ed.startKeyCapture()}>{#if ed.capturing === 'key'}<span class="accent">Press a key…</span>{:else if ed.builder.key}{ed.builder.key}{:else}<span class="muted">Click &amp; press key</span>{/if}</button>
            <button onclick={() => ed.addKey('key_press')}>Press</button>
          </div>
          <div class="button-grid"><button onclick={() => ed.addKey('key_down')}>Key Down</button><button onclick={() => ed.addKey('key_up')}>Key Up</button></div>
        </section>
        <section class="inline"><h3 class="side">Wait</h3>
          <input class="input" type="number" min="1" aria-label="Wait ms" bind:value={ed.builder.delay} /><span class="muted">ms</span>
          <button onclick={() => ed.addDelay()}>Wait</button></section>
        <section class="inline"><h3 class="side">Loop</h3>
          <input class="input" type="number" min="0" title="0 = infinite" aria-label="Loop count" bind:value={ed.builder.loop} />
          <button onclick={() => ed.addLoop()}>Start Loop</button><button onclick={() => ed.addSimple('loop_end')}>End Loop</button></section>
        <section>
          <h3>Lock</h3>
          <div class="button-grid">{#each locks as [label, type] (type)}<button onclick={() => ed.addSimple(type)}>{label}</button>{/each}</div>
        </section>
      </div>

      <div class="card action-list">
        <h3>Actions</h3>
        <div class="list" role="listbox" aria-label="Actions">
          {#each sc.actions as a, i (i)}
            <div class="action-row" class:selected={ed.selected === i} role="option" aria-selected={ed.selected === i} tabindex="0"
              onclick={() => ed.selectAction(i)} ondblclick={() => ed.editSelected()}
              onkeydown={(e) => { if (e.key === 'Enter') ed.editSelected(); else if (e.key === ' ') { e.preventDefault(); ed.selectAction(i); } }}>
              <span class="num">{num(i)}</span>{ed.labels[i] ?? a.type}
            </div>
          {/each}
        </div>
        <div class="action-tools">
          <button disabled={ed.selected < 0} onclick={() => ed.editSelected()}>Edit</button>
          <button disabled={ed.selected < 0} onclick={() => ed.deleteSelected()}>Delete</button>
          <button disabled={ed.selected <= 0} onclick={() => ed.move(-1)}>Up</button>
          <button disabled={ed.selected < 0 || ed.selected >= sc.actions.length - 1} onclick={() => ed.move(1)}>Down</button>
        </div>
      </div>
    </div>

    <div class="editor-foot"><button onclick={() => ed.openRaw()}>Raw JSON</button><span class="spacer"></span>
      <button class="primary" onclick={() => ed.close()}>Close</button></div>
  </main>
  <ActionDialog {ed} />
  <RawEditorDialog {ed} />
  <ConflictDialog {ed} />
  <DiscardDialog {ed} />
{/if}
