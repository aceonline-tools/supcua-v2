import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/client';
import { MockBackend } from '../api/mock';
import type { AppStore } from './store.svelte';
import { editorStore } from './test-editor';

class FlakyBackend extends MockBackend {
  override async call(method: string, params: unknown): Promise<unknown> {
    const r = super.call(method, params);
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
const cid = () => mock.currentCaptureId;
const flush = () => new Promise((r) => setTimeout(r, 0));
const count = (m: string) => mock.calls.filter((c) => c.method === m).length;

beforeEach(async () => {
  mock = new GatedBackend();
  store = await editorStore(mock, 's3');
});

describe('hotkey capture', () => {
  it('assigns a captured hotkey, then stops the capture and resumes mouse', async () => {
    await store.editor.startCapture('toggle');
    expect(store.editor.capturing).toBe('toggle');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'toggle', hotkey: { modifiers: ['ctrl'], key: 'k' }, display: 'Ctrl + K' });
    expect(store.editor.script?.toggle_hotkey).toEqual({ modifiers: ['ctrl'], key: 'k' });
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    expect(store.editor.capturing).toBeNull();
    expect(mock.calls.find((c) => c.method === 'capture.stop')?.params).toMatchObject({ capture_id: 2 });
  });

  it('cancels on Esc without changing the hotkey and stops the capture', async () => {
    await store.editor.startCapture('toggle');
    mock.push('capture.hotkey', { capture_id: cid(), field: 'toggle', hotkey: { modifiers: [], key: 'esc' }, display: 'Esc' });
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    expect(store.editor.capturing).toBeNull();
    expect(store.editor.script?.toggle_hotkey).toEqual({ modifiers: [], key: 'f1' });
    expect(count('capture.stop')).toBe(1);
    expect(store.editor.dirty).toBe(false);
  });

  it('explicit cancel stops the capture and resumes mouse once', async () => {
    await store.editor.startKeyCapture();
    await store.editor.cancelCapture();
    await flush();
    expect(count('capture.stop')).toBe(1);
    expect(count('capture.mouse.start')).toBe(2);
  });

  it('ignores mouse captures while an input has focus, otherwise updates X/Y', async () => {
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
    mock.failStart = true;
    await store.editor.startKeyCapture();
    expect(store.editor.capturing).toBeNull();
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    mock.failStart = false;
  });

  it('a timeout that overtakes the start reply is applied after the reply', async () => {
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
    mock.push('capture.ended', { capture_id: cid(), reason: 'timeout' });
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(2));
    mock.push('capture.ended', { capture_id: cid(), reason: 'blur' });
    await flush();
    expect(count('capture.mouse.start')).toBe(2);
    window.dispatchEvent(new Event('focus'));
    await vi.waitFor(() => expect(count('capture.mouse.start')).toBe(3));
  });

  it('drops malformed capture events before dispatch', async () => {
    mock.push('capture.mouse', { x: 5, y: 5 }); // no capture_id
    mock.push('capture.mouse', { capture_id: cid(), x: 'a', y: 1 });
    expect(store.editor.mouse).toEqual({ x: 0, y: 0 });
  });
});


describe('insert caret', () => {
  it('insertAt tracks the insert index after select, insert, delete, move, moveTo', async () => {
    const ed = store.editor;
    const n = () => ed.script!.actions.length;
    expect(ed.selected).toBe(-1);
    expect(ed.insertAt).toBe(n());
    ed.selectAction(2);
    expect(ed.insertAt).toBe(3);
    await ed.move(1);
    expect(ed.insertAt).toBe(4);
    await ed.moveTo(3, 0);
    expect(ed.insertAt).toBe(1);
    const len = n();
    await ed.addSimple('mouse_left_click');
    expect(ed.script!.actions[1].type).toBe('mouse_left_click');
    expect(n()).toBe(len + 1);
    expect(ed.insertAt).toBe(ed.selected + 1);
    await ed.deleteSelected();
    expect(ed.insertAt).toBe(ed.selected + 1);
    ed.selected = -1;
    await ed.addSimple('mouse_right_click');
    expect(ed.script!.actions[n() - 1].type).toBe('mouse_right_click');
  });

  it('is 0 for an empty list', async () => {
    mock = new GatedBackend();
    store = await editorStore(mock, null);
    expect(store.editor.script!.actions).toHaveLength(0);
    expect(store.editor.insertAt).toBe(0);
  });
});

describe('actions', () => {
  it('reorder calls describeActions exactly once and swaps locally', async () => {
    store.editor.selectAction(1);
    const before = count('editor.describeActions');
    await store.editor.move(-1);
    expect(count('editor.describeActions') - before).toBe(1);
    expect(store.editor.script?.actions[0].type).toBe('mouse_move');
    expect(store.editor.selected).toBe(0);
    expect(store.editor.labels[0]).toBe('Mouse Move → (640, 360)');
  });

  it('inserts after the selection and edits through the dialog', async () => {
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
    store.editor.selectAction(2);
    store.editor.editSelected();
    expect(store.editor.actionDialog).toBeNull();
  });
});


describe('queued capture requests keep their own intent', () => {
  it('delayed A + queued B (timeout variant): the timeout replayed for A does not clear B', async () => {
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
