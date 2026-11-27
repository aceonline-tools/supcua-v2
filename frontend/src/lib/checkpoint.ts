export const CHECKPOINT_DEBOUNCE_MS = 500;
export const CHECKPOINT_MAX_WAIT_MS = 2000;
export const MAX_DRAFT_BYTES = 1 << 20;

// Debounced draft checkpoints (contract §6.2): 500 ms trailing, 2 s max-wait, one send in flight.
// `send` reads the current revision/dirty state when it runs, so a late timer never sends stale data.
export class CheckpointSender {
  private debounce: ReturnType<typeof setTimeout> | null = null;
  private maxWait: ReturnType<typeof setTimeout> | null = null;
  private inflight = false;
  private again = false;
  private stopped = false;

  constructor(private send: () => Promise<void>, private debounceMs = CHECKPOINT_DEBOUNCE_MS, private maxWaitMs = CHECKPOINT_MAX_WAIT_MS) {}

  touch(): void {
    if (this.stopped) return;
    if (this.debounce) clearTimeout(this.debounce);
    this.debounce = setTimeout(() => this.fire(), this.debounceMs);
    if (!this.maxWait) this.maxWait = setTimeout(() => this.fire(), this.maxWaitMs);
  }

  cancel(): void {
    if (this.debounce) clearTimeout(this.debounce);
    if (this.maxWait) clearTimeout(this.maxWait);
    this.debounce = this.maxWait = null;
    this.again = false;
  }

  stop(): void {
    this.stopped = true;
    this.cancel();
  }

  private fire(): void {
    this.cancel();
    if (this.stopped) return;
    if (this.inflight) { this.again = true; return; }
    this.inflight = true;
    void this.send().catch(() => {}).finally(() => {
      this.inflight = false;
      if (this.again && !this.stopped) { this.again = false; this.touch(); }
    });
  }
}

export function draftBytes(script: unknown): number {
  return new TextEncoder().encode(JSON.stringify(script)).length;
}
