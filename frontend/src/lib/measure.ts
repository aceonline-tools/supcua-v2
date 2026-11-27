import type { Client } from '../api/client';

const MAX_ROUNDS = 4;
const clampPx = (v: number) => Math.min(8192, Math.max(1, Math.ceil(v)));

export interface MeasureEnv {
  root: () => HTMLElement | null;
  ready: () => Promise<void>;
  frames?: () => Promise<void>;
  fonts?: () => Promise<unknown>;
  dpr?: () => number;
}

export interface MeasureMetrics {
  cssW: number;
  cssH: number;
  dpr: number;
  scrollW: number;
  clientW: number;
  scrollH: number;
  clientH: number;
  lastBlockBottom: number;
}

const twoFrames = () => new Promise<void>((res) => requestAnimationFrame(() => requestAnimationFrame(() => res())));

// Page side of the size handshake (contract §5.2). The host drives it: measureRequest{token} starts a
// measurement, the measured reply says show or remeasure (new token + width), measureAbort ends it.
export class MeasureController {
  private token = 0;
  private epoch = 0;
  private measuring = false;
  last: MeasureMetrics | null = null;

  constructor(private api: Client, private env: MeasureEnv) {
    document.documentElement.dataset.measure = '';
    this.measuring = true;
  }

  get active(): boolean { return this.measuring; }

  metrics(): MeasureMetrics | null {
    const el = this.env.root();
    if (!el) return null;
    const r = el.getBoundingClientRect();
    const kids = Array.from(el.children) as HTMLElement[];
    const bottom = kids.reduce((m, k) => Math.max(m, k.getBoundingClientRect().bottom), r.top);
    return {
      cssW: clampPx(r.width), cssH: clampPx(r.height), dpr: (this.env.dpr ?? (() => window.devicePixelRatio || 1))(),
      scrollW: el.scrollWidth, clientW: el.clientWidth, scrollH: el.scrollHeight, clientH: el.clientHeight,
      lastBlockBottom: Math.round(bottom - r.top),
    };
  }

  // Every request and every abort starts a new epoch. An operation acts (measures, reports, leaves, acks)
  // only while its epoch is current, on success and error paths alike, so a superseded or aborted
  // measurement can neither clear measure mode for a newer token nor ack an obsolete one.
  async request(token: number): Promise<void> {
    const mine = ++this.epoch;
    const live = () => mine === this.epoch;
    this.token = token;
    this.measuring = true;
    document.documentElement.dataset.measure = '';
    try {
      await this.env.ready();
      await (this.env.fonts ?? (() => document.fonts?.ready))();
      await (this.env.frames ?? twoFrames)();
    } catch { /* measure what is there */ }
    let t = token;
    for (let round = 0; round < MAX_ROUNDS; round++) {
      if (!live()) return;
      const m = this.metrics();
      if (!m) return this.finish(mine, t);
      this.last = m;
      let reply: { next: 'show' | 'remeasure'; token?: number; cssW?: number };
      try {
        reply = await this.api.call('editor.measured', { token: t, cssW: m.cssW, cssH: m.cssH, dpr: m.dpr });
      } catch {
        return this.finish(mine, t);
      }
      if (!live()) return;
      if (reply.next !== 'remeasure' || reply.token === undefined || reply.cssW === undefined) return this.finish(mine, t);
      t = this.token = reply.token;
      document.documentElement.style.setProperty('--measure-w', `${reply.cssW}px`);
      document.documentElement.dataset.measureFit = '';
      await (this.env.frames ?? twoFrames)();
    }
    return this.finish(mine, t);
  }

  abort(): Promise<void> {
    this.epoch++;
    return this.leave(this.token);
  }

  private async finish(mine: number, token: number): Promise<void> {
    if (mine !== this.epoch) return;
    this.epoch++;
    await this.leave(token);
  }

  private async leave(token: number): Promise<void> {
    this.measuring = false;
    delete document.documentElement.dataset.measure;
    delete document.documentElement.dataset.measureFit;
    document.documentElement.style.removeProperty('--measure-w');
    await this.api.call('editor.measureAck', { token }).catch(() => {});
  }
}
