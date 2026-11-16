import { beforeEach, describe, expect, it } from 'vitest';
import { ApiError, createClient } from '../api/client';
import { MockBackend } from '../api/mock';
import { AppStore } from './store.svelte';

class FlakyBackend extends MockBackend {
  failClose = false;
  override async call(method: string, params: unknown): Promise<unknown> {
    const r = super.call(method, params);
    if (method === 'editor.close' && this.failClose) { await r; throw new ApiError({ code: 'INTERNAL', message: 'Internal error' }); }
    return r;
  }
}

let mock: FlakyBackend;
let store: AppStore;
const count = (m: string) => mock.calls.filter((c) => c.method === m).length;

beforeEach(async () => {
  mock = new FlakyBackend();
  store = new AppStore(createClient(mock));
  await store.init();
});

describe('editor lifecycle', () => {
  it('open then clean close releases the session and returns to the list', async () => {
    await store.openEditor('s3');
    expect(store.route).toEqual({ name: 'editor', id: 's3' });
    expect(store.editor.indicator).toEqual({ saved: true, text: 'Saved' });
    expect(await store.editor.close()).toBe('closed');
    expect(count('editor.close')).toBe(1);
    expect(store.route.name).toBe('scripts');
  });

  it('always calls editor.close, even when the close call itself errors', async () => {
    await store.openEditor('s3');
    mock.failClose = true;
    expect(await store.editor.close()).toBe('closed');
    expect(count('editor.close')).toBe(1);
    expect(store.editor.sessionId).toBeNull();
    expect(store.route.name).toBe('scripts');
  });

  it('closes without saving when the name is empty (parity), and reports why', async () => {
    await store.openEditor('s3');
    store.editor.script!.name = '  ';
    store.editor.markDirty();
    expect(store.editor.indicator.text).toBe('Not saved: name required');
    await store.editor.close();
    expect(count('editor.close')).toBe(1);
    expect(store.error).toContain('name required');
    expect(store.scripts.find((s) => s.id === 's3')?.name).toBe('Auto attack');
  });

  it('keeps the editor open once on a hard save failure, then closes anyway', async () => {
    await store.openEditor('s3');
    mock.failSaveFor.add('s3');
    store.editor.markDirty();
    expect(await store.editor.close()).toBe('failed');
    expect(store.editor.indicator.text).toBe('Not saved: save failed');
    expect(count('editor.close')).toBe(0);
    expect(await store.editor.close()).toBe('closed');
    expect(count('editor.close')).toBe(1);
  });

  it('a second editor.open reports EDITOR_BUSY without changing the session', async () => {
    await store.openEditor('s3');
    const sid = store.editor.sessionId;
    await expect(createClient(mock).call('editor.open', { id: 's1' })).rejects.toMatchObject({ code: 'EDITOR_BUSY' });
    expect(store.editor.sessionId).toBe(sid);
  });

  it('retains no draft after close', async () => {
    await store.openEditor('s3');
    store.editor.builder.key = 'x';
    await store.editor.addSimple('mouse_left_click');
    await store.editor.close();
    const ed = store.editor;
    expect(ed.script).toBeNull();
    expect(ed.catalog).toEqual([]);
    expect(ed.labels).toEqual([]);
    expect(ed.sessionId).toBeNull();
    expect(ed.builder.key).toBe('');
    expect(ed.dirty).toBe(false);
    expect(ed.raw).toBeNull();
  });
});

describe('autosave conflicts', () => {
  it('shows the conflict modal, stays open on No, and overrides on Yes', async () => {
    await store.openEditor('s3');
    store.editor.script!.toggle_hotkey = { modifiers: [], key: 'f5' };
    store.editor.markDirty();
    expect(await store.editor.close()).toBe('conflict');
    expect(store.editor.conflicts?.[0]).toMatchObject({ scriptId: 's1', field: 'toggle_hotkey' });
    expect(count('editor.close')).toBe(0);

    await store.editor.resolveConflict(false);
    expect(store.editor.conflicts).toBeNull();
    expect(store.route.name).toBe('editor');

    expect(await store.editor.close()).toBe('conflict');
    expect(await store.editor.resolveConflict(true)).toBe('closed');
    expect(mock.calls.filter((c) => c.method === 'editor.save').pop()?.params).toMatchObject({ overrideConflicts: true });
    expect(count('editor.close')).toBe(1);
    expect(store.scripts.find((s) => s.id === 's1')?.hotkey_label).toBe('(none)');
    expect(store.scripts.find((s) => s.id === 's3')?.hotkey_label).toBe('F5');
  });
});

describe('hotkey capture', () => {
  it('assigns a captured hotkey', async () => {
    await store.openEditor('s3');
    await store.editor.startCapture('toggle');
    expect(store.editor.capturing).toBe('toggle');
    mock.push('capture.hotkey', { field: 'toggle', hotkey: { modifiers: ['ctrl'], key: 'k' }, display: 'Ctrl + K' });
    expect(store.editor.script?.toggle_hotkey).toEqual({ modifiers: ['ctrl'], key: 'k' });
    expect(store.editor.capturing).toBeNull();
    expect(store.editor.dirty).toBe(true);
  });

  it('cancels on Esc without changing the hotkey and stops the capture', async () => {
    await store.openEditor('s3');
    await store.editor.startCapture('toggle');
    mock.push('capture.hotkey', { field: 'toggle', hotkey: { modifiers: [], key: 'esc' }, display: 'Esc' });
    await Promise.resolve();
    expect(store.editor.capturing).toBeNull();
    expect(store.editor.script?.toggle_hotkey).toEqual({ modifiers: [], key: 'f1' });
    expect(count('capture.stop')).toBe(1);
    expect(store.editor.dirty).toBe(false);
  });

  it('explicit cancel stops the capture; capture.ended clears state', async () => {
    await store.openEditor('s3');
    await store.editor.startKeyCapture();
    await store.editor.cancelCapture();
    expect(count('capture.stop')).toBe(1);
    await store.editor.startCapture('start');
    mock.push('capture.ended', { reason: 'blur' });
    expect(store.editor.capturing).toBeNull();
  });

  it('ignores mouse captures while an input has focus, otherwise updates X/Y', async () => {
    await store.openEditor('s3');
    mock.push('capture.mouse', { x: 640, y: 360 });
    expect(store.editor.mouse).toEqual({ x: 640, y: 360 });
    const input = document.createElement('input');
    document.body.append(input);
    input.focus();
    mock.push('capture.mouse', { x: 1, y: 2 });
    expect(store.editor.mouse).toEqual({ x: 640, y: 360 });
    input.remove();
  });
});

describe('actions', () => {
  it('reorder calls describeActions exactly once and swaps locally', async () => {
    await store.openEditor('s3');
    store.editor.selectAction(1);
    const before = count('editor.describeActions');
    await store.editor.move(-1);
    expect(count('editor.describeActions') - before).toBe(1);
    expect(store.editor.script?.actions[0].type).toBe('mouse_move');
    expect(store.editor.selected).toBe(0);
    expect(store.editor.labels[0]).toBe('Mouse Move → (640, 360)');
  });

  it('inserts after the selection and edits through the dialog', async () => {
    await store.openEditor('s3');
    store.editor.selectAction(4);
    await store.editor.addDelay();
    expect(store.editor.script?.actions[5]).toEqual({ type: 'delay', ms: 100 });
    store.editor.editSelected();
    expect(store.editor.actionDialog?.spec.type).toBe('delay');
    await store.editor.applyActionDialog({ type: 'delay', ms: 250 });
    expect(store.editor.script?.actions[5].ms).toBe(250);
    expect(store.editor.labels[5]).toBe('Delay: 250ms');
  });

  it('click actions have no parameter dialog', async () => {
    await store.openEditor('s3');
    store.editor.selectAction(2);
    store.editor.editSelected();
    expect(store.editor.actionDialog).toBeNull();
  });
});

describe('license and update', () => {
  it('update dialog tracks progress and skip', async () => {
    mock.push('update.available', { tag: 'v1.0.29' });
    expect(store.update?.tag).toBe('v1.0.29');
    await store.applyUpdate();
    mock.push('update.progress', { pct: 40 });
    expect(store.update).toMatchObject({ applying: true, pct: 40 });
    mock.push('update.failed', { message: 'Download failed' });
    expect(store.update).toMatchObject({ applying: false, failed: 'Download failed' });
    await store.skipUpdate();
    expect(store.update).toBeNull();
    expect(count('update.skip')).toBe(1);
  });
});
