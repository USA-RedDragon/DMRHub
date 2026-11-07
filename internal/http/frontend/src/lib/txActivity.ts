// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

/** TxMode is how a map pin blips: in a loop while a call is on the air, or once for a data burst. */
export type TxMode = 'loop' | 'burst';

/** CALL_TIMEOUT_MS is how long a call stays on the air after its last frame when no end frame arrives. */
export const CALL_TIMEOUT_MS = 5000;

/** BURST_MS is how long the single blip of a data burst lasts. */
export const BURST_MS = 1000;

/** DOT_MS is how long the dot of a data call takes to travel from the sender to the receiver. */
export const DOT_MS = 900;

/** DATA_LINE_MS is how long the line of a data call stays before it fades. */
export const DATA_LINE_MS = 1800;

const MEMORY_MS = 60000;

const VOICE_KINDS = new Set(['', 'voice']);
const DATA_KINDS = new Set(['data', 'gps', 'udt', 'csbk', 'alias', 'unknown']);

/** TxFrame is the part of a call row of /ws/calls or /ws/lastheard that the blips and lines read. */
export type TxFrame = {
  id: number;
  active?: boolean;
  kind?: string;
  sub_kind?: string;
  source_id?: number;
  user?: { id?: number };
  repeater_id?: number;
  group_call?: boolean;
  destination_id?: number;
  is_to_talkgroup?: boolean;
  to_talkgroup?: { id?: number };
  is_to_repeater?: boolean;
  to_repeater?: { id?: number };
  is_to_user?: boolean;
};

/** TxReceiver is a radio or a repeater on the map that a data call goes to. */
export type TxReceiver = { kind: 'radio' | 'repeater'; id: number };

/** TxCall is a call that is on the air, or a data call whose line still shows. */
export type TxCall = { frame: TxFrame; data: boolean };

type Blip = { radio: number; repeater: number; mode: TxMode; from: number; until: number };
type Call = TxCall & { until: number };

/** isTxFrame reports whether a websocket frame is a call row. */
export function isTxFrame(frame: unknown): frame is TxFrame {
  const f = frame as Partial<TxFrame> | null;
  return !!f && typeof f === 'object' && typeof f.id === 'number';
}

/** isDataFrame reports whether a call row is a data call, which blips once instead of in a loop. */
export function isDataFrame(frame: TxFrame): boolean {
  return DATA_KINDS.has(frame.kind ?? '');
}

/**
 * TxActivity tracks which radios and repeaters are transmitting, from the
 * call rows of the call websockets and from the position reports, and the
 * calls whose lines show.
 */
export class TxActivity {
  private blips = new Map<string, Blip>();
  private calls = new Map<string, Call>();
  private done = new Map<string, number>();
  private seen = 0;

  /**
   * call applies one call row at now, and reports whether the blips or
   * the lines may have changed. A data call rings receivers once when its
   * dot arrives.
   */
  call(frame: TxFrame, now: number, receivers: TxReceiver[] = []): boolean {
    const kind = frame.kind ?? '';
    const key = `call:${frame.id}`;
    const radio = frame.source_id || frame.user?.id || 0;
    const repeater = frame.repeater_id ?? 0;
    this.forget(now);
    if (VOICE_KINDS.has(kind)) {
      if (!frame.active) {
        this.done.set(key, now + MEMORY_MS);
        const ended = this.calls.delete(key);
        return this.blips.delete(key) || ended;
      }
      if (this.done.has(key)) {
        return false;
      }
      const had = this.blips.get(key);
      this.blips.set(key, { radio, repeater, mode: 'loop', from: had?.from ?? now, until: now + CALL_TIMEOUT_MS });
      this.calls.set(key, { frame, data: false, until: now + CALL_TIMEOUT_MS });
      return !had || had.radio !== radio || had.repeater !== repeater;
    }
    if (!DATA_KINDS.has(kind) || !this.burst(key, radio, repeater, now)) {
      return false;
    }
    this.calls.set(key, { frame, data: true, until: now + DATA_LINE_MS });
    for (const r of receivers) {
      const at = now + DOT_MS;
      this.blips.set(`${key}:${r.kind}:${r.id}`, {
        radio: r.kind === 'radio' ? r.id : 0,
        repeater: r.kind === 'repeater' ? r.id : 0,
        mode: 'burst',
        from: at,
        until: at + BURST_MS,
      });
    }
    return true;
  }

  /** position applies a position report of radio, heard through repeater, at now. */
  position(radio: number, repeater: number, receivedAt: string, now: number): boolean {
    this.forget(now);
    return this.burst(`pos:${radio}:${receivedAt}`, radio, repeater, now);
  }

  /** expire drops what ended by now, and reports whether anything ended or started since the last expire. */
  expire(now: number): boolean {
    let changed = false;
    for (const [key, b] of this.blips) {
      if (b.until <= now) {
        this.blips.delete(key);
        if (b.mode === 'loop') {
          this.done.set(key, now + MEMORY_MS);
        }
        changed = true;
      } else if (b.from > this.seen && b.from <= now) {
        changed = true;
      }
    }
    for (const [key, c] of this.calls) {
      if (c.until <= now) {
        this.calls.delete(key);
        changed = true;
      }
    }
    this.forget(now);
    return changed;
  }

  /** next is the time of the next start or end after now, or null when nothing is on the air. */
  next(now: number): number | null {
    let at: number | null = null;
    const consider = (t: number) => {
      if (t > now && (at === null || t < at)) {
        at = t;
      }
    };
    for (const b of this.blips.values()) {
      consider(b.from);
      consider(b.until);
    }
    for (const c of this.calls.values()) {
      consider(c.until);
    }
    return at;
  }

  /** radios maps each visible radio that blips at now to how it blips. */
  radios(visible: ReadonlySet<number>, now: number): Record<number, TxMode> {
    return this.pick(visible, now, (b) => b.radio);
  }

  /** repeaters maps each visible repeater that blips at now to how it blips. */
  repeaters(visible: ReadonlySet<number>, now: number): Record<number, TxMode> {
    return this.pick(visible, now, (b) => b.repeater);
  }

  /** active lists the calls whose lines show at now. */
  active(now: number): TxCall[] {
    const out: TxCall[] = [];
    for (const c of this.calls.values()) {
      if (c.until > now) {
        out.push({ frame: c.frame, data: c.data });
      }
    }
    return out;
  }

  private burst(key: string, radio: number, repeater: number, now: number): boolean {
    if (this.done.has(key) || this.blips.has(key)) {
      return false;
    }
    this.done.set(key, now + MEMORY_MS);
    this.blips.set(key, { radio, repeater, mode: 'burst', from: now, until: now + BURST_MS });
    return true;
  }

  private pick(visible: ReadonlySet<number>, now: number, id: (b: Blip) => number): Record<number, TxMode> {
    const out: Record<number, TxMode> = {};
    for (const b of this.blips.values()) {
      const at = id(b);
      if (at === 0 || !visible.has(at) || b.from > now || b.until <= now) {
        continue;
      }
      if (out[at] !== 'loop') {
        out[at] = b.mode;
      }
    }
    return out;
  }

  private forget(now: number) {
    this.seen = Math.max(this.seen, now);
    for (const [key, until] of this.done) {
      if (until <= now) {
        this.done.delete(key);
      }
    }
  }
}
