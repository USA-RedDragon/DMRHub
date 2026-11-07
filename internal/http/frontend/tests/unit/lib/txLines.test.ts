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

import { describe, it, expect } from 'vitest';
import type { TxFrame } from '@/lib/txActivity';
import { curvePath, MAX_LINES_PER_CALL, resolveLines, type TxPoint, type TxRepeaterPoint } from '@/lib/txLines';

const BLUE = '#2563eb';
const RED = '#dc2626';
const ALICE = 3009991;
const BOB = 3009992;
const NOPOS = 3009995;
const RPT_A = 300999901;
const RPT_B = 300999903;
const TG = 3100;

const radios = new Map<number, TxPoint>([
  [ALICE, { longitude: -97.7729, latitude: 30.2669, color: BLUE }],
  [BOB, { longitude: -97.7475, latitude: 30.2500, color: BLUE }],
]);

const repeater = (longitude: number, talkgroups: number[]): TxRepeaterPoint => ({ longitude, latitude: 30.26, color: RED, talkgroups });

const repeaters = new Map<number, TxRepeaterPoint>([
  [RPT_A, repeater(-97.77, [TG])],
  [RPT_B, repeater(-97.74, [TG, 3101])],
]);

const tgCall = (over: Partial<TxFrame> = {}): TxFrame => ({
  id: 1,
  active: true,
  kind: 'voice',
  source_id: ALICE,
  repeater_id: RPT_A,
  group_call: true,
  is_to_talkgroup: true,
  to_talkgroup: { id: TG },
  destination_id: TG,
  ...over,
});

const privateCall = (over: Partial<TxFrame> = {}): TxFrame => ({
  id: 2,
  active: true,
  kind: 'voice',
  source_id: ALICE,
  repeater_id: RPT_A,
  group_call: false,
  is_to_user: true,
  destination_id: BOB,
  ...over,
});

const ends = (frame: TxFrame, r = radios, p = repeaters) =>
  resolveLines(frame, r, p).map((l) => [`${l.from.kind}:${l.from.id}`, `${l.to.kind}:${l.to.id}`, l.color]);

describe('resolveLines', () => {
  it('draws a talkgroup call from the radio to every other repeater that carries the talkgroup', () => {
    expect(ends(tgCall())).toEqual([[`radio:${ALICE}`, `repeater:${RPT_B}`, BLUE]]);
  });

  it('starts at the repeater when the radio has no position on the map', () => {
    expect(ends(tgCall({ source_id: NOPOS, repeater_id: RPT_B }))).toEqual([[`repeater:${RPT_B}`, `repeater:${RPT_A}`, RED]]);
  });

  it('draws a private call between two radios on the map', () => {
    expect(ends(privateCall())).toEqual([[`radio:${ALICE}`, `radio:${BOB}`, BLUE]]);
  });

  it('draws a call to a repeater', () => {
    const f = privateCall({ is_to_user: false, is_to_repeater: true, to_repeater: { id: RPT_B }, destination_id: RPT_B });
    expect(ends(f)).toEqual([[`radio:${ALICE}`, `repeater:${RPT_B}`, BLUE]]);
  });

  it('draws no line when an end is not on the map', () => {
    expect(ends(privateCall({ destination_id: NOPOS }))).toEqual([]);
    expect(ends(tgCall({ source_id: NOPOS, repeater_id: 300888801 }))).toEqual([]);
    expect(ends(tgCall({ to_talkgroup: { id: 9 }, destination_id: 9 }))).toEqual([]);
    expect(ends(tgCall(), radios, new Map([[RPT_A, repeaters.get(RPT_A)!]]))).toEqual([]);
    expect(ends(privateCall(), new Map([[ALICE, radios.get(ALICE)!]]))).toEqual([]);
  });

  it('draws no line for a call the Last Heard page hides', () => {
    expect(ends(tgCall({ kind: 'idle' }))).toEqual([]);
    expect(ends(tgCall({ kind: 'usbd' }))).toEqual([]);
    expect(ends(tgCall({ kind: 'unknown' }))).toEqual([]);
  });

  it('draws no line for a group call to a local group or a plugin', () => {
    expect(ends(tgCall({ is_to_talkgroup: false }))).toEqual([]);
  });

  it('draws the same data line whatever the data subtype', () => {
    const want = [[`radio:${ALICE}`, `radio:${BOB}`, BLUE]];
    for (const over of [{ sub_kind: 'sms' }, { sub_kind: 'lrrp' }, { sub_kind: 'tms' }, { sub_kind: '' }]) {
      expect(ends(privateCall({ kind: 'data', active: false, ...over }))).toEqual(want);
    }
    expect(ends(privateCall({ kind: 'data', sub_kind: 'sms', destination_id: NOPOS }))).toEqual([]);
  });

  it(`caps a talkgroup fan-out at ${MAX_LINES_PER_CALL} lines`, () => {
    const many = new Map<number, TxRepeaterPoint>();
    for (let i = 10; i > 0; i--) {
      many.set(300999900 + i, repeater(-97.7 - i / 100, [TG]));
    }
    const lines = resolveLines(tgCall({ repeater_id: 300999901 }), radios, many);
    expect(lines).toHaveLength(MAX_LINES_PER_CALL);
    expect(lines.map((l) => l.to.id)).toEqual([300999902, 300999903, 300999904, 300999905, 300999906, 300999907]);
    expect(new Set(lines.map((l) => l.key)).size).toBe(MAX_LINES_PER_CALL);
  });
});

describe('curvePath', () => {
  it('bows the curve and trims it off the pins', () => {
    expect(curvePath({ x: 0, y: 0 }, { x: 100, y: 0 }, 10, 10)).toBe('M 9.4 3.4 Q 50.0 18.0 90.6 3.4');
  });

  it('bows a long curve by at most 80 pixels', () => {
    expect(curvePath({ x: 0, y: 0 }, { x: 1000, y: 0 }, 0, 0)).toBe('M 0.0 0.0 Q 500.0 80.0 1000.0 0.0');
  });

  it('is empty when the pins touch', () => {
    expect(curvePath({ x: 0, y: 0 }, { x: 15, y: 0 }, 11, 12)).toBe('');
  });
});
