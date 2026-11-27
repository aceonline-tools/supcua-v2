import { describe, expect, it } from 'vitest';
import css from '../app.css?raw';
import overlay from '../components/OverlayStyle.svelte?raw';
import keyseq from '../components/KeySequenceInput.svelte?raw';

describe('layout stability rules', () => {
  it('reserves scrollbar space on every scroll container', () => {
    for (const s of ['main', '.editor-main', '.action-list .list', '.combo-list', '.dialog-body', 'textarea']) {
      expect(css).toMatch(new RegExp(`${s.replace('.', '\\.')}[^{]*\\{scrollbar-gutter:stable`));
    }
    expect(keyseq).toContain('scrollbar-gutter: stable');
  });
  it('uses tabular numerals and a fixed slot for readouts', () => {
    expect(css).toMatch(/body\{[^}]*font-variant-numeric:tabular-nums/);
    expect(overlay).toMatch(/\.ov-pct \{ min-width: 42px; text-align: right/);
  });
  it('keeps banners and inline errors out of the flow', () => {
    expect(css).toMatch(/\.banner\{position:fixed/);
    expect(css).toMatch(/\.msg-float\{position:absolute/);
    expect(css).toMatch(/\.msg-slot\{min-height/);
  });
  it('never changes box metrics on selection', () => {
    expect(css).not.toMatch(/\.(nav\.active|chosen|selected)\{[^}]*(font-weight|border-width|padding|margin)/);
  });
});
