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

const MEMORY_MS = 60000;

const LOOP_KINDS = new Set(['', 'voice']);
const BURST_KINDS = new Set(['data', 'gps', 'udt', 'csbk', 'alias', 'unknown']);

/** TxFrame is the part of a call row of /ws/calls or /ws/lastheard that the blips read. */
export type TxFrame = {
  id: number;
  active?: boolean;
  kind?: string;
  source_id?: number;
  user?: { id?: number };
  repeater_id?: number;
};

type Entry = { radio: number; repeater: number; mode: TxMode; until: number };

/** isTxFrame reports whether a websocket frame is a call row. */
export function isTxFrame(frame: unknown): frame is TxFrame {
  const f = frame as Partial<TxFrame> | null;
  return !!f && typeof f === 'object' && typeof f.id === 'number';
}

/**
 * TxActivity tracks which radios and repeaters are transmitting, from the
 * call rows of the call websockets and from the position reports.
 */
export class TxActivity {
  private entries = new Map<string, Entry>();
  private done = new Map<string, number>();

  /** call applies one call row at now, and reports whether the blips may have changed. */
  call(frame: TxFrame, now: number): boolean {
    const kind = frame.kind ?? '';
    const key = `call:${frame.id}`;
    const radio = frame.source_id || frame.user?.id || 0;
    const repeater = frame.repeater_id ?? 0;
    this.forget(now);
    if (LOOP_KINDS.has(kind)) {
      if (!frame.active) {
        this.done.set(key, now + MEMORY_MS);
        return this.entries.delete(key);
      }
      if (this.done.has(key)) {
        return false;
      }
      const had = this.entries.get(key);
      this.entries.set(key, { radio, repeater, mode: 'loop', until: now + CALL_TIMEOUT_MS });
      return !had || had.radio !== radio || had.repeater !== repeater;
    }
    if (BURST_KINDS.has(kind)) {
      return this.burst(key, radio, repeater, now);
    }
    return false;
  }

  /** position applies a position report of radio, heard through repeater, at now. */
  position(radio: number, repeater: number, receivedAt: string, now: number): boolean {
    this.forget(now);
    return this.burst(`pos:${radio}:${receivedAt}`, radio, repeater, now);
  }

  /** expire drops what ended by now, and reports whether anything did. */
  expire(now: number): boolean {
    let changed = false;
    for (const [key, e] of this.entries) {
      if (e.until <= now) {
        this.entries.delete(key);
        if (e.mode === 'loop') {
          this.done.set(key, now + MEMORY_MS);
        }
        changed = true;
      }
    }
    this.forget(now);
    return changed;
  }

  /** next is the time of the next expiry, or null when nothing is on the air. */
  next(): number | null {
    let at: number | null = null;
    for (const e of this.entries.values()) {
      if (at === null || e.until < at) {
        at = e.until;
      }
    }
    return at;
  }

  /** radios maps each visible radio that is transmitting to how it blips. */
  radios(visible: ReadonlySet<number>): Record<number, TxMode> {
    return this.pick(visible, (e) => e.radio);
  }

  /** repeaters maps each visible repeater that carries a transmission to how it blips. */
  repeaters(visible: ReadonlySet<number>): Record<number, TxMode> {
    return this.pick(visible, (e) => e.repeater);
  }

  private burst(key: string, radio: number, repeater: number, now: number): boolean {
    if (this.done.has(key) || this.entries.has(key)) {
      return false;
    }
    this.done.set(key, now + MEMORY_MS);
    this.entries.set(key, { radio, repeater, mode: 'burst', until: now + BURST_MS });
    return true;
  }

  private pick(visible: ReadonlySet<number>, id: (e: Entry) => number): Record<number, TxMode> {
    const out: Record<number, TxMode> = {};
    for (const e of this.entries.values()) {
      const at = id(e);
      if (at === 0 || !visible.has(at)) {
        continue;
      }
      if (out[at] !== 'loop') {
        out[at] = e.mode;
      }
    }
    return out;
  }

  private forget(now: number) {
    for (const [key, until] of this.done) {
      if (until <= now) {
        this.done.delete(key);
      }
    }
  }
}
