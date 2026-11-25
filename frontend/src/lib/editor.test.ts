import { beforeEach, describe, expect, it, vi } from 'vitest';
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

// Holds the reply of chosen methods after the host has already handled them (events can overtake replies).
class GatedBackend extends FlakyBackend {
  holds = new Map<string, Promise<void>>();
  failStart = false;
  override async call(method: string, params: unknown): Promise<unknown> {
    if (this.failStart && method === 'capture.key.start') {
      this.push('capture.ended', { reason: 'stopped', capture_id: this.currentCaptureId }); // Start ends the old capture, then fails
      throw new ApiError({ code: 'INTERNAL', message: 'Internal error' });
    }
    let r: unknown;
    try {
      r = await super.call(method, params);
    } catch (e) {
      await this.holds.get(method);
      throw e;
    }
    await this.holds.get(method);
    return r;
  }
}

let mock: GatedBackend;
let store: AppStore;
const count = (m: string) => mock.calls.filter((c) => c.method === m).length;

beforeEach(async () => {
  mock = new GatedBackend();
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

  it('keeps the draft when the name is invalid on close, until the user discards', async () => {
    await store.openEditor('s3');
    store.editor.script!.name = '  ';
    store.editor.markDirty();
    expect(store.editor.indicator.text).toBe('Not saved: name required');
    expect(await store.editor.close()).toBe('failed');
    expect(count('editor.close')).toBe(0);
    expect(store.editor.sessionId).not.toBeNull();
    expect(store.editor.script!.name).toBe('  ');
    expect(store.editor.discardPrompt).toContain('name required');
    expect(await store.editor.close()).toBe('failed');
    expect(count('editor.close')).toBe(0);
    await store.editor.discardAndClose();
    expect(count('editor.close')).toBe(1);
    expect(store.route.name).toBe('scripts');
    expect(store.scripts.find((s) => s.id === 's3')?.name).toBe('Auto attack');
  });

  it('repeated failed Close keeps the draft until an explicit discard', async () => {
    await store.openEditor('s3');
    mock.failSaveFor.add('s3');
    store.editor.script!.name = 'Renamed';
    store.editor.markDirty();
    for (let i = 0; i < 3; i++) {
      expect(await store.editor.close()).toBe('failed');
      expect(store.editor.indicator.text).toBe('Not saved: save failed');
      expect(store.editor.discardPrompt).not.toBeNull();
      expect(count('editor.close')).toBe(0);
      expect(store.editor.script!.name).toBe('Renamed');
      store.editor.keepEditing();
      expect(store.editor.discardPrompt).toBeNull();
    }
    await store.editor.close();
    await store.editor.discardAndClose();
    expect(count('editor.close')).toBe(1);
    expect(store.editor.sessionId).toBeNull();
  });

  it('a failed save on navigate away keeps the pending route for discard and clears it on keep editing', async () => {
    await store.openEditor('s3');
    mock.failSaveFor.add('s3');
    store.editor.markDirty();
    expect(await store.navigate({ name: 'settings' })).toBe(false);
    expect(store.route.name).toBe('editor');
    await store.editor.discardAndClose();
    expect(store.route.name).toBe('settings');
    await store.openEditor('s3');
    store.editor.markDirty();
    expect(await store.navigate({ name: 'settings' })).toBe(false);
    store.editor.keepEditing();
    expect(store.route.name).toBe('editor');
  });

  it('an untouched new script with no name still closes silently', async () => {
    await store.openNew();
    expect(await store.editor.close()).toBe('closed');
  });

  it('an edit during a pending NAME_EMPTY save-on-close is never silently discarded', async () => {
    await store.openNew();
    let release!: () => void;
    mock.holds.set('editor.save', new Promise<void>((r) => (release = r)));
    const closing = store.editor.close();
    await vi.waitFor(() => expect(count('editor.save')).toBe(1));
    store.editor.script!.name = ''; store.editor.script!.actions = [...store.editor.script!.actions];
    store.editor.markDirty();
    mock.holds.delete('editor.save');
    release();
    expect(await closing).toBe('failed');
    expect(store.editor.sessionId).not.toBeNull();
    expect(store.editor.discardPrompt).toBeTruthy();
  });

  it('an edit made during an in-flight save-on-close is saved, not overwritten', async () => {
    await store.openEditor('s3');
    store.editor.script!.name = 'First';
    store.editor.markDirty();
    let release!: () => void;
    mock.holds.set('editor.save', new Promise<void>((r) => (release = r)));
    const closing = store.editor.close();
    await vi.waitFor(() => expect(count('editor.save')).toBe(1));
    store.editor.script!.name = 'Second';
    store.editor.markDirty();
    mock.holds.delete('editor.save');
    release();
    expect(await closing).toBe('closed');
    expect(count('editor.save')).toBe(2);
    expect(mock.calls.filter((c) => c.method === 'editor.save').pop()?.params).toMatchObject({ script: { name: 'Second' } });
    expect(store.scripts.find((s) => s.id === 's3')?.name).toBe('Second');
  });

  it('an edit during a save that then fails keeps the newer draft open', async () => {
    await store.openEditor('s3');
    store.editor.script!.name = 'First';
    store.editor.markDirty();
    let release!: () => void;
    mock.holds.set('editor.save', new Promise<void>((r) => (release = r)));
    const closing = store.editor.close();
    await vi.waitFor(() => expect(count('editor.save')).toBe(1));
    store.editor.script!.name = 'Second';
    store.editor.markDirty();
    mock.holds.delete('editor.save');
    mock.failSaveFor.add('s3');
    release();
    expect(await closing).toBe('failed');
    expect(store.editor.script!.name).toBe('Second');
    expect(store.editor.dirty).toBe(true);
    expect(count('editor.close')).toBe(0);
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

const cid = () => mock.currentCaptureId;
const flush = () => new Promise((r) => setTimeout(r, 0));

describe('hotkey capture', () => {
  it('assigns a captured hotkey, then stops the capture and resumes mouse', async () => {
    await store.openEditor('s3');
    await store.editor.startCapture('toggle');
    expect(store.editor.capturing).toBe('toggle');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'toggle', hotkey: { modifiers: ['ctrl'], key: 'k' }, display: 'Ctrl + K' });
    expect(store.editor.script?.toggle_hotkey).toEqual({ modifiers: ['ctrl'], key: 'k' });
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    expect(store.editor.capturing).toBeNull();
    expect(mock.calls.find((c) => c.method === 'capture.stop')?.params).toMatchObject({ capture_id: 2 });
  });

  it('cancels on Esc without changing the hotkey and stops the capture', async () => {
    await store.openEditor('s3');
    await store.editor.startCapture('toggle');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'toggle', hotkey: { modifiers: [], key: 'esc' }, display: 'Esc' });
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    expect(store.editor.capturing).toBeNull();
    expect(store.editor.script?.toggle_hotkey).toEqual({ modifiers: [], key: 'f1' });
    expect(count('capture.stop')).toBe(1);
    expect(store.editor.dirty).toBe(false);
  });

  it('explicit cancel stops the capture and resumes mouse once', async () => {
    await store.openEditor('s3');
    await store.editor.startKeyCapture();
    await store.editor.cancelCapture();
    await flush();
    expect(count('capture.stop')).toBe(1);
    expect(count('capture.mouse.start')).toBe(2);
  });

  it('ignores mouse captures while an input has focus, otherwise updates X/Y', async () => {
    await store.openEditor('s3');
    mock.push('capture.mouse', { capture_id: cid(), x: 640, y: 360 });
    expect(store.editor.mouse).toEqual({ x: 640, y: 360 });
    const input = document.createElement('input');
    document.body.append(input);
    input.focus();
    mock.push('capture.mouse', { capture_id: cid(), x: 1, y: 2 });
    expect(store.editor.mouse).toEqual({ x: 640, y: 360 });
    input.remove();
  });
});

describe('capture ids and delayed transport', () => {
  it('a late timeout for the previous capture does not cancel a new keyboard capture', async () => {
    await store.openEditor('s3');
    const oldId = cid();
    await store.editor.startKeyCapture();
    expect(cid()).toBe(oldId + 1);
    mock.push('capture.ended', { capture_id: oldId, reason: 'timeout' }); // stale
    mock.push('capture.mouse', { capture_id: oldId, x: 9, y: 9 }); // stale
    await flush();
    expect(store.editor.capturing).toBe('key');
    expect(count('capture.mouse.start')).toBe(1);
    expect(store.editor.mouse).toEqual({ x: 0, y: 0 });
    mock.push('capture.key', { capture_id: cid(), key: 'q' });
    expect(store.editor.builder.key).toBe('q');
  });

  it('a failed start clears the capture state and resumes mouse picking', async () => {
    await store.openEditor('s3');
    mock.failStart = true;
    await store.editor.startKeyCapture();
    expect(store.editor.capturing).toBeNull();
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    mock.failStart = false;
  });

  it('a timeout that overtakes the start reply is applied after the reply', async () => {
    await store.openEditor('s3');
    let release!: () => void;
    mock.holds.set('capture.key.start', new Promise<void>((r) => (release = r)));
    const started = store.editor.startKeyCapture();
    await vi.waitFor(() => expect(count('capture.key.start')).toBe(1));
    mock.push('capture.ended', { capture_id: cid(), reason: 'timeout' }); // arrives before the reply
    expect(store.editor.capturing).toBe('key'); // not acted on yet
    release();
    await started;
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    expect(store.editor.capturing).toBeNull();
  });

  it('a key event that overtakes the start reply is buffered and applied', async () => {
    await store.openEditor('s3');
    let release!: () => void;
    mock.holds.set('capture.key.start', new Promise<void>((r) => (release = r)));
    const started = store.editor.startKeyCapture();
    await vi.waitFor(() => expect(count('capture.key.start')).toBe(1));
    mock.push('capture.key', { capture_id: cid(), key: 'z' });
    release();
    await started;
    expect(store.editor.builder.key).toBe('z');
  });

  it('timeout resumes mouse; blur waits for re-activation', async () => {
    await store.openEditor('s3');
    mock.push('capture.ended', { capture_id: cid(), reason: 'timeout' });
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    mock.push('capture.ended', { capture_id: cid(), reason: 'blur' });
    await flush();
    expect(count('capture.mouse.start')).toBe(2);
    window.dispatchEvent(new Event('focus'));
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(3));
  });

  it('drops malformed capture events before dispatch', async () => {
    await store.openEditor('s3');
    mock.push('capture.mouse', { x: 5, y: 5 }); // no capture_id
    mock.push('capture.mouse', { capture_id: cid(), x: 'a', y: 1 });
    expect(store.editor.mouse).toEqual({ x: 0, y: 0 });
  });
});

describe('editor.reset', () => {
  it('drops the draft and returns to the list; stale resets are ignored', async () => {
    await store.openEditor('s3');
    mock.push('editor.reset', { reason: 'navigation', session_id: 'old-session' });
    expect(store.route.name).toBe('editor');
    mock.push('editor.reset', { reason: 'navigation', session_id: store.editor.sessionId });
    expect(store.route.name).toBe('scripts');
    expect(store.editor.sessionId).toBeNull();
    expect(store.editor.script).toBeNull();
    expect(store.error).toContain('reset');
  });
});

describe('close attempts that leave the editor open', () => {
  it('reconciles and resumes mouse picking after a conflict', async () => {
    await store.openEditor('s3');
    store.editor.script!.toggle_hotkey = { modifiers: [], key: 'f5' };
    store.editor.markDirty();
    let release!: () => void;
    mock.holds.set('editor.save', new Promise<void>((r) => (release = r)));
    const closing = store.editor.close();
    await vi.waitFor(() => expect(count('editor.save')).toBe(1));
    mock.push('capture.ended', { capture_id: cid(), reason: 'timeout' }); // resume is blocked while closing
    await flush();
    expect(count('capture.mouse.start')).toBe(1);
    release();
    expect(await closing).toBe('conflict');
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    mock.holds.delete('editor.save');
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

const seq = () => mock.calls.map((c) => c.method);

describe('leaving the editor', () => {
  it('nav.goto saves and closes before navigating', async () => {
    await store.openEditor('s3');
    store.editor.script!.name = 'Renamed';
    store.editor.markDirty();
    mock.push('nav.goto', { route: 'settings' });
    await vi.waitFor(() => expect(store.route.name).toBe('settings'));
    expect(seq().indexOf('editor.save')).toBeLessThan(seq().indexOf('editor.close'));
    expect(store.scripts.find((s) => s.id === 's3')?.name).toBe('Renamed');
  });

  it('nav.goto stays in the editor while a hotkey conflict is unresolved, then continues to the target', async () => {
    await store.openEditor('s3');
    store.editor.script!.toggle_hotkey = { modifiers: [], key: 'f5' };
    store.editor.markDirty();
    mock.push('nav.goto', { route: 'settings' });
    await vi.waitFor(() => expect(store.editor.conflicts).not.toBeNull());
    expect(store.route.name).toBe('editor');
    await store.editor.resolveConflict(true);
    expect(store.route.name).toBe('settings');
  });

  it('a second open while a session exists is reported, not ignored', async () => {
    await store.openEditor('s3');
    await store.openEditor('s1');
    expect(store.error).toContain('already open');
    expect(store.editor.script?.id).toBe('s3');
  });
});

describe('new scripts', () => {
  it('are saved on close even if untouched, with a default name', async () => {
    await store.openNew();
    expect(store.editor.dirty).toBe(true);
    store.editor.script!.name = 'Brand new';
    await store.editor.close();
    expect(store.scripts.some((s) => s.name === 'Brand new')).toBe(true);
  });

  it('an untouched nameless new script closes quietly', async () => {
    await store.openNew();
    await store.editor.close();
    expect(store.error).toBe('');
    expect(store.route.name).toBe('scripts');
  });
});

describe('native close', () => {
  it('app.closing with a dirty editor saves, closes, then reports ready (in order)', async () => {
    await store.openEditor('s3');
    store.editor.script!.name = 'Edited on exit';
    store.editor.markDirty();
    mock.push('app.closing', { deadline_ms: 2000 });
    await vi.waitFor(() => expect(count('app.closingReady')).toBe(1));
    const s = seq();
    expect(s.indexOf('editor.save')).toBeLessThan(s.indexOf('editor.close'));
    expect(s.indexOf('editor.close')).toBeLessThan(s.indexOf('app.closingReady'));
    expect(store.scripts.find((x) => x.id === 's3')?.name).toBe('Edited on exit');
  });

  it('still reports ready when there is no editor, and when save fails', async () => {
    mock.push('app.closing', { deadline_ms: 2000 });
    await vi.waitFor(() => expect(count('app.closingReady')).toBe(1));
    expect(count('editor.save')).toBe(0);

    const m2 = new FlakyBackend();
    const s2 = new AppStore(createClient(m2));
    await s2.init();
    await s2.openEditor('s3');
    m2.failSaveFor.add('s3');
    s2.editor.markDirty();
    m2.push('app.closing', { deadline_ms: 2000 });
    await vi.waitFor(() => expect(m2.calls.some((c) => c.method === 'app.closingReady')).toBe(true));
    expect(m2.calls.some((c) => c.method === 'editor.close')).toBe(true);
  });
});

describe('native close with a close already running', () => {
  it('ignores conflicts, always releases the session, then reports ready', async () => {
    await store.openEditor('s3');
    store.editor.script!.toggle_hotkey = { modifiers: [], key: 'f5' };
    store.editor.markDirty();
    let release!: () => void;
    mock.holds.set('editor.save', new Promise<void>((r) => (release = r)));
    const closing = store.editor.close();
    await vi.waitFor(() => expect(count('editor.save')).toBe(1));
    mock.push('app.closing', { deadline_ms: 2000 });
    release();
    expect(await closing).toBe('conflict');
    mock.holds.delete('editor.save');
    await vi.waitFor(() => expect(count('app.closingReady')).toBe(1));
    const saves = mock.calls.filter((c) => c.method === 'editor.save');
    expect(saves.at(-1)?.params).toMatchObject({ ignore_conflicts: true });
    const s = seq();
    expect(s.indexOf('editor.close')).toBeGreaterThan(-1);
    expect(s.lastIndexOf('editor.save')).toBeLessThan(s.indexOf('editor.close'));
    expect(s.indexOf('editor.close')).toBeLessThan(s.indexOf('app.closingReady'));
    expect(store.scripts.find((x) => x.id === 's1')?.hotkey_label).toBe('F5'); // other script untouched
    expect(store.scripts.find((x) => x.id === 's3')?.hotkey_label).toBe('F5'); // duplicate kept
  });
});

describe('queued capture requests keep their own intent', () => {
  it('delayed A + queued B (timeout variant): the timeout replayed for A does not clear B', async () => {
    await store.openEditor('s3');
    let release!: () => void;
    mock.holds.set('capture.key.start', new Promise<void>((r) => (release = r)));
    const a = store.editor.startKeyCapture();
    const b = store.editor.startCapture('toggle'); // queued behind A
    await vi.waitFor(() => expect(count('capture.key.start')).toBe(1));
    mock.push('capture.ended', { capture_id: cid(), reason: 'timeout' }); // A's timeout, buffered until A's reply
    release();
    await Promise.all([a, b]);
    mock.holds.clear();
    expect(store.editor.capturing).toBe('toggle');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'toggle', hotkey: { modifiers: [], key: 'f9' }, display: 'F9' });
    expect(store.editor.script?.toggle_hotkey?.key).toBe('f9');
  });

  it('failing A + queued B (start-failure variant): B still gets its intent and its events', async () => {
    await store.openEditor('s3');
    mock.failStart = true;
    const a = store.editor.startKeyCapture();
    const b = store.editor.startCapture('start');
    await Promise.all([a, b]);
    mock.failStart = false;
    expect(store.editor.capturing).toBe('start');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'start', hotkey: { modifiers: [], key: 'f8' }, display: 'F8' });
    expect(store.editor.script?.start_hotkey?.key).toBe('f8');
  });

  it('a deferred stop scheduled for A does not stop B', async () => {
    await store.openEditor('s3');
    let release!: () => void;
    mock.holds.set('capture.key.start', new Promise<void>((r) => (release = r)));
    const a = store.editor.startKeyCapture();
    const b = store.editor.startCapture('toggle');
    await vi.waitFor(() => expect(count('capture.key.start')).toBe(1));
    mock.push('capture.key', { capture_id: cid(), key: 'z' }); // A accepted; its stop is queued behind B
    release();
    await Promise.all([a, b]);
    mock.holds.clear();
    await new Promise((r) => setTimeout(r, 0));
    expect(store.editor.builder.key).toBe('z');
    expect(count('capture.stop')).toBe(0);
    expect(store.editor.capturing).toBe('toggle');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'toggle', hotkey: { modifiers: [], key: 'f7' }, display: 'F7' });
    expect(store.editor.script?.toggle_hotkey?.key).toBe('f7');
  });

  it('cancel requested while a start is queued stops that start with its own id, not an earlier one', async () => {
    await store.openEditor('s3');
    let release!: () => void;
    mock.holds.set('capture.key.start', new Promise<void>((r) => (release = r)));
    const a = store.editor.startKeyCapture();
    const b = store.editor.startCapture('toggle');
    await vi.waitFor(() => expect(count('capture.key.start')).toBe(1));
    const c = store.editor.cancelCapture(); // targets B
    release();
    await Promise.all([a, b, c]);
    mock.holds.clear();
    const stops = mock.calls.filter((m) => m.method === 'capture.stop');
    expect(stops).toHaveLength(1);
    expect((stops[0].params as { capture_id: number }).capture_id).toBe(3); // mouse=1, A=2, B=3
    expect(store.editor.capturing).toBeNull();
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
  });
});
