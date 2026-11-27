<script lang="ts">
  import type { AppStore } from '../lib/store.svelte';
  import type { ThemeSetting } from '../api/types';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  import Toggle from './Toggle.svelte';
  import HotkeyRecorder from './HotkeyRecorder.svelte';
  import TextSetting from './TextSetting.svelte';
  import OverlayStyle from './OverlayStyle.svelte';
  let { store }: { store: AppStore } = $props();

  const themes: { id: ThemeSetting; label: string; icon: IconName }[] = [
    { id: 'dark', label: 'Dark', icon: 'moon' }, { id: 'light', label: 'Light', icon: 'sun' }, { id: 'system', label: 'System', icon: 'monitor' },
  ];
  const cfg = $derived(store.settings);
  const aa = $derived(store.state?.auto_active);
  const ov = $derived(store.state?.overlay);
</script>

<div class="page-head"><div><h1>Settings</h1><p>Appearance and game focus behavior.</p></div></div>

<div class="card settings-card">
  <div class="section-title"><h2>Appearance</h2></div>
  <div class="setting-row">
    <div><strong>Theme</strong><p>Choose how supcua looks on your desktop.</p></div>
    <div class="segments" role="radiogroup" aria-label="Theme">
      {#each themes as t (t.id)}
        <button role="radio" aria-checked={store.themeSetting === t.id} class:chosen={store.themeSetting === t.id}
          onclick={() => store.setTheme(t.id)}><Icon name={t.icon} size={13} />{t.label}</button>
      {/each}
    </div>
  </div>
</div>

{#if aa}
  <div class="card settings-card">
    <div class="section-title"><h2>Game focus</h2></div>
    <div class="setting-row">
      <div><strong>Auto-active when game focused</strong><p>Automatically activate when the game has focus.</p></div>
      <Toggle checked={aa.enabled} label="Auto-active when game focused" onchange={(v) => store.setAutoActive({ enabled: v })} />
    </div>
    <div class="setting-row">
      <div><strong>Game process name</strong><p>Executable that must be in the foreground.</p></div>
      <TextSetting label="Game process name" value={aa.process_name} commit={(v) => store.setAutoActive({ processName: String(v) })} />
    </div>
  </div>
{/if}

{#if ov}
  <div class="card settings-card">
    <div class="section-title"><h2>Overlay</h2></div>
    <div class="setting-row">
      <div><strong>Overlay enabled</strong><p>Show the on-screen status overlay feature.</p></div>
      <Toggle checked={ov.enabled} label="Overlay enabled" onchange={(v) => store.setOverlay({ enabled: v })} />
    </div>
    <div class="setting-row">
      <div><strong>Overlay visible</strong></div>
      <Toggle checked={ov.visible} label="Overlay visible" disabled={!ov.enabled} onchange={(v) => store.setOverlay({ visible: v })} />
    </div>
    <div class="setting-row">
      <div><strong>Overlay draggable</strong></div>
      <Toggle checked={ov.draggable} label="Overlay draggable" disabled={!ov.enabled} onchange={(v) => store.setOverlay({ draggable: v })} />
    </div>
    {#if cfg}
      <div class="setting-row">
        <div><strong>Overlay hotkey</strong></div>
        <HotkeyRecorder label="Overlay hotkey" field="overlay_hotkey" value={cfg.overlay_hotkey} api={store.api} save={(d) => store.updateSettings({ overlay_hotkey: d })} />
      </div>
    {/if}
    <OverlayStyle {store} />
  </div>
{/if}

{#if cfg}
  <div class="card settings-card">
    <div class="section-title"><h2>Configuration</h2></div>
    <div class="setting-row">
      <div><strong>Global hotkey</strong></div>
      <HotkeyRecorder label="Global hotkey" field="global_hotkey" value={cfg.global_hotkey} api={store.api} save={(d) => store.updateSettings({ global_hotkey: d })} />
    </div>
    <div class="setting-row">
      <div><strong>Capture hotkey</strong><p>Captures the cursor position.</p></div>
      <HotkeyRecorder label="Capture hotkey" field="capture_hotkey" value={cfg.capture_hotkey} api={store.api} save={(d) => store.updateSettings({ capture_hotkey: d })} />
    </div>
    <div class="setting-row">
      <div>
        <strong>Key press duration (ms)</strong><span class="tag" class:pending={store.restartPending.includes('key_press_duration_ms')}>takes effect after restart</span>
      </div>
      <TextSetting label="Key press duration" numeric value={cfg.key_press_duration_ms} commit={(v) => store.updateSettings({ key_press_duration_ms: Number(v) })} />
    </div>
    <div class="setting-row">
      <div>
        <strong>Scripts directory</strong><span class="tag" class:pending={store.restartPending.includes('scripts_dir')}>restart required</span>
      </div>
      <TextSetting label="Scripts directory" value={cfg.scripts_dir} commit={(v) => store.updateSettings({ scripts_dir: String(v) })} />
    </div>
  </div>
{/if}

<div class="card settings-card">
  <div class="section-title"><h2>License</h2></div>
  <div class="setting-row">
    <div><strong>{store.boot?.mode === 'main' ? 'Licensed' : 'Not licensed'}</strong>
      <p>Machine {store.boot?.macs[0] ?? '—'} · {store.boot?.version ?? ''} · WebView2 {store.boot?.runtimeVersion ?? '—'}</p></div>
  </div>
</div>
