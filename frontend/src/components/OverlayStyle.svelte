<script lang="ts">
  import type { AppStore, SettingResult } from '../lib/store.svelte';
  import type { OverlayStyleDTO } from '../api/types';
  import TextSetting from './TextSetting.svelte';
  import { OVERLAY_LIMITS, STATUS_TEXT_FALLBACK, isHex, maxRadius, parseOverlayNumber } from '../lib/overlayStyle';
  let { store }: { store: AppStore } = $props();

  const st = $derived(store.overlayStyle);
  const pct = (v: number) => Math.round(v * 100);

  type NumField = 'radius' | 'width' | 'height' | 'x' | 'y';
  async function commitNumber(field: NumField, raw: string | number): Promise<SettingResult> {
    if (!st) return { ok: false, field };
    const v = parseOverlayNumber(field, raw, st);
    if (v === null) return { ok: false, field };
    const patch: Partial<OverlayStyleDTO> = { [field]: v };
    if (field === 'width' || field === 'height') {
      const w = field === 'width' ? v : st.width, h = field === 'height' ? v : st.height;
      if (st.radius > maxRadius(w, h)) patch.radius = maxRadius(w, h);
    }
    await store.setOverlayStyle(patch);
    return { ok: true };
  }
  async function commitHex(field: 'background' | 'text_color', raw: string | number): Promise<SettingResult> {
    const v = String(raw).trim();
    if (!(isHex(v) || (field === 'text_color' && v === ''))) return { ok: false, field };
    await store.setOverlayStyle({ [field]: v.toUpperCase() });
    return { ok: true };
  }
</script>

{#if st}
  <div class="ov-style" data-testid="overlay-style">
    <div class="setting-row sub"><div><strong>Style</strong><p>Changes apply to the running overlay immediately.</p></div>
      <button onclick={() => store.resetOverlayStyle()}>Reset to defaults</button></div>
    <div class="setting-row">
      <div><strong>Corner radius</strong><p>0 – {maxRadius(st.width, st.height)} px</p></div>
      <span class="ov-num"><TextSetting label="Corner radius" numeric value={st.radius} commit={(v) => commitNumber('radius', v)} /></span>
    </div>
    <div class="setting-row">
      <div><strong>Background colour</strong></div>
      <div class="ctl">
        <input type="color" aria-label="Background colour" value={st.background.toLowerCase()}
          oninput={(e) => store.setOverlayStyle({ background: e.currentTarget.value.toUpperCase() })} />
        <span class="ov-hex"><TextSetting label="Background hex" value={st.background} commit={(v) => commitHex('background', v)} /></span>
      </div>
    </div>
    <div class="setting-row">
      <div><strong>Text colour</strong><p>Empty = status colours (green / gray / red).</p></div>
      <div class="ctl">
        <input type="color" aria-label="Text colour" value={(st.text_color || STATUS_TEXT_FALLBACK).toLowerCase()}
          oninput={(e) => store.setOverlayStyle({ text_color: e.currentTarget.value.toUpperCase() })} />
        <span class="ov-hex"><TextSetting label="Text hex" placeholder="auto" value={st.text_color} commit={(v) => commitHex('text_color', v)} /></span>
      </div>
    </div>
    <div class="setting-row">
      <div><strong>Background opacity</strong><p>Only the background fades; text stays solid.</p></div>
      <div class="ctl">
        <input type="range" min="0" max="100" step="1" aria-label="Background opacity" value={pct(st.background_opacity)}
          oninput={(e) => store.setOverlayStyle({ background_opacity: Number(e.currentTarget.value) / 100 })} />
        <span class="ov-pct" data-testid="bg-opacity-label">{pct(st.background_opacity)}%</span>
      </div>
    </div>
    <div class="setting-row">
      <div><strong>Overall opacity</strong><p>Everything fades together (min {pct(OVERLAY_LIMITS.minOpacity)}%).</p></div>
      <div class="ctl">
        <input type="range" min={pct(OVERLAY_LIMITS.minOpacity)} max="100" step="1" aria-label="Overall opacity" value={pct(st.opacity)}
          oninput={(e) => store.setOverlayStyle({ opacity: Number(e.currentTarget.value) / 100 })} />
        <span class="ov-pct" data-testid="opacity-label">{pct(st.opacity)}%</span>
      </div>
    </div>
    <div class="setting-row">
      <div><strong>Size (px)</strong><p>Width {OVERLAY_LIMITS.minWidth}–{OVERLAY_LIMITS.maxWidth} · height {OVERLAY_LIMITS.minHeight}–{OVERLAY_LIMITS.maxHeight}</p></div>
      <div class="ctl">
        <span class="ov-num"><TextSetting label="Width" numeric value={st.width} commit={(v) => commitNumber('width', v)} /></span>
        <span aria-hidden="true">×</span>
        <span class="ov-num"><TextSetting label="Height" numeric value={st.height} commit={(v) => commitNumber('height', v)} /></span>
      </div>
    </div>
    <div class="setting-row">
      <div><strong>Position (px)</strong><p>Screen coordinates: 0,0 is the primary monitor's top-left (the virtual desktop can be negative). Dragging updates these.</p></div>
      <div class="ctl">
        <span class="ov-num"><TextSetting label="X" numeric value={st.x} commit={(v) => commitNumber('x', v)} /></span>
        <span class="ov-num"><TextSetting label="Y" numeric value={st.y} commit={(v) => commitNumber('y', v)} /></span>
      </div>
    </div>
  </div>
{/if}

<style>
  .ov-style { border-top: 1px solid var(--line); }
  .ov-style :global(.ov-num .input) { width: 84px; }
  .ov-style :global(.ov-hex .input) { width: 92px; font-family: var(--mono, monospace); }
  .ov-pct { min-width: 38px; text-align: right; color: var(--muted); font-variant-numeric: tabular-nums; }
  input[type='color'] { width: 32px; height: var(--ctl-h); padding: 0; border: 1px solid var(--line); border-radius: var(--radius); background: var(--bg); }
  input[type='range'] { width: 150px; accent-color: var(--accent); }
</style>
