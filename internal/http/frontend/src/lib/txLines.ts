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

import { defaultFilter, matchesFilter, type CallRow } from '@/lib/transmissions';
import type { TxFrame } from '@/lib/txActivity';

/** MAX_LINES_PER_CALL caps the lines of one call, so that a talkgroup on many repeaters does not flood the map. */
export const MAX_LINES_PER_CALL = 6;

/** RADIO_PIN_RADIUS and REPEATER_PIN_RADIUS are the radii in pixels of the map pins, white edge included. */
export const RADIO_PIN_RADIUS = 11;
export const REPEATER_PIN_RADIUS = 12;

const MAX_BEND = 80;
const BEND = 0.18;

/** TxPoint is a radio or a repeater on the map. */
export type TxPoint = { longitude: number; latitude: number; color: string };

/** TxRepeaterPoint is a repeater on the map, with the talkgroups it carries. */
export type TxRepeaterPoint = TxPoint & { talkgroups?: number[] };

/** TxEnd is an end of a line: a radio or a repeater on the map. */
export type TxEnd = { kind: 'radio' | 'repeater'; id: number; longitude: number; latitude: number };

/** TxLine is a line from the source of a call to one of its destinations. */
export type TxLine = { key: string; from: TxEnd; to: TxEnd; color: string };

/**
 * resolveLines lists the lines of the call row frame, from the radio that
 * sends it, or the repeater it came in on when the radio is not on the
 * map, to the radio it calls, the repeater it calls, or the repeaters that
 * carry its talkgroup. It draws only what the Last Heard page shows, and
 * only between points on the map.
 */
export function resolveLines(
  frame: TxFrame,
  radios: ReadonlyMap<number, TxPoint>,
  repeaters: ReadonlyMap<number, TxRepeaterPoint>,
): TxLine[] {
  if (!matchesFilter(frame as unknown as CallRow, defaultFilter())) {
    return [];
  }
  const source = sourceOf(frame, radios, repeaters);
  if (!source) {
    return [];
  }
  return destinationsOf(frame, radios, repeaters)
    .filter((to) => to.kind !== source.end.kind || to.id !== source.end.id)
    .slice(0, MAX_LINES_PER_CALL)
    .map((to) => ({ key: `${frame.id}:${to.kind}:${to.id}`, from: source.end, to, color: source.color }));
}

function end(kind: TxEnd['kind'], id: number, p: TxPoint): TxEnd {
  return { kind, id, longitude: p.longitude, latitude: p.latitude };
}

function sourceOf(
  frame: TxFrame,
  radios: ReadonlyMap<number, TxPoint>,
  repeaters: ReadonlyMap<number, TxRepeaterPoint>,
): { end: TxEnd; color: string } | null {
  const radio = frame.source_id || frame.user?.id || 0;
  const r = radios.get(radio);
  if (r) {
    return { end: end('radio', radio, r), color: r.color };
  }
  const repeater = frame.repeater_id ?? 0;
  const p = repeaters.get(repeater);
  return p ? { end: end('repeater', repeater, p), color: p.color } : null;
}

function destinationsOf(
  frame: TxFrame,
  radios: ReadonlyMap<number, TxPoint>,
  repeaters: ReadonlyMap<number, TxRepeaterPoint>,
): TxEnd[] {
  const dest = frame.destination_id ?? 0;
  if (frame.is_to_repeater) {
    const id = frame.to_repeater?.id || dest;
    const p = repeaters.get(id);
    return p ? [end('repeater', id, p)] : [];
  }
  if (frame.is_to_talkgroup) {
    const tg = frame.to_talkgroup?.id || dest;
    return [...repeaters.entries()]
      .filter(([id, p]) => id !== frame.repeater_id && p.talkgroups?.includes(tg))
      .sort(([a], [b]) => a - b)
      .map(([id, p]) => end('repeater', id, p));
  }
  if (!frame.group_call) {
    const p = radios.get(dest);
    return p ? [end('radio', dest, p)] : [];
  }
  return [];
}

type Pixel = { x: number; y: number };

function toward(p: Pixel, q: Pixel, by: number): Pixel {
  const len = Math.hypot(q.x - p.x, q.y - p.y) || 1;
  return { x: p.x + ((q.x - p.x) / len) * by, y: p.y + ((q.y - p.y) / len) * by };
}

/**
 * curvePath is the SVG path of a curve from a to b in pixels, bowed by 18%
 * of its length up to 80 pixels, that starts trimA pixels out of a and ends
 * trimB pixels short of b, so that it does not cover the pins. It is empty
 * when the pins touch.
 */
export function curvePath(a: Pixel, b: Pixel, trimA: number, trimB: number): string {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len = Math.hypot(dx, dy);
  if (len <= trimA + trimB) {
    return '';
  }
  const bend = Math.min(MAX_BEND, len * BEND);
  const c = { x: (a.x + b.x) / 2 - (dy / len) * bend, y: (a.y + b.y) / 2 + (dx / len) * bend };
  const s = toward(a, c, trimA);
  const e = toward(b, c, trimB);
  const n = (v: number) => v.toFixed(1);
  return `M ${n(s.x)} ${n(s.y)} Q ${n(c.x)} ${n(c.y)} ${n(e.x)} ${n(e.y)}`;
}
