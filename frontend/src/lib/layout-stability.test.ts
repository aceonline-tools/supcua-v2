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
  it('bounds message slots and dialog bodies', () => {
    expect(css).toMatch(/\.msg-slot\{height:18px;[^}]*overflow:auto/);
    expect(css).toMatch(/\.msg-slot\.one\{height:24px/);
    expect(css).toMatch(/\.msg-slot\.two\{height:48px/);
    expect(css).toMatch(/\.dialog-body\{[^}]*max-height:calc\(100vh[^}]*overflow:auto/);
  });
  it('stacks notifications in one region below the modal layer', () => {
    expect(css).toMatch(/\.notices\{position:fixed;[^}]*z-index:30;[^}]*flex-direction:column/);
    expect(css).toMatch(/\.modal-back\{z-index:100/);
    expect(css).toMatch(/main\{padding:[^}]*var\(--notice-h/);
  });
  it('keeps banners and inline errors out of the flow', () => {
        expect(css).toMatch(/\.msg-float\{position:absolute/);
    expect(css).toMatch(/\.msg-slot\{height/);
  });
  it('the editor root never scrolls: the grid shrinks and the form columns and the list scroll themselves', () => {
    expect(css).toMatch(/\.editor-main>\.editor-grid\{flex:1 1 0;min-height:0;grid-template-rows:minmax\(0,1fr\)\}/);
    expect(css).toMatch(/\.editor-card\{overflow-y:auto\}/);
    expect(css).toMatch(/\.action-list \.list\{[^}]*overflow-y:auto/);
    expect(css).toMatch(/html\[data-measure\] \.editor-main>\.editor-grid\{flex:none/);
    expect(css).toMatch(/html\[data-measure\] \.editor-card\{overflow:visible\}/);
  });
  it('never changes box metrics on selection', () => {
    expect(css).not.toMatch(/\.(nav\.active|chosen|selected)\{[^}]*(font-weight|border-width|padding|margin)/);
  });
});
