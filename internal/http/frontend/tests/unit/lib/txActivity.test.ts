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
import {
  BURST_MS,
  CALL_TIMEOUT_MS,
  DATA_LINE_MS,
  DOT_MS,
  isDataFrame,
  isTxFrame,
  TxActivity,
  type TxFrame,
} from '@/lib/txActivity';

const RADIO = 3109999;
const OTHER = 3109998;
const REPEATER = 310999901;
const radios = new Set([RADIO, OTHER]);
const repeaters = new Set([REPEATER]);

const voice = (over: Partial<TxFrame> = {}): TxFrame => ({
  id: 7,
  active: true,
  kind: 'voice',
  source_id: RADIO,
  repeater_id: REPEATER,
  ...over,
});

const data = (over: Partial<TxFrame> = {}): TxFrame => voice({ id: 8, kind: 'data', sub_kind: 'sms', active: false, ...over });

describe('TxActivity', () => {
  it('blips a radio and its repeater from call start until call end', () => {
    const a = new TxActivity();
    expect(a.call(voice(), 1000)).toBe(true);
    expect(a.radios(radios, 1000)).toEqual({ [RADIO]: 'loop' });
    expect(a.repeaters(repeaters, 1000)).toEqual({ [REPEATER]: 'loop' });
    expect(a.active(1000).map((c) => [c.frame.id, c.data])).toEqual([[7, false]]);
    expect(a.call(voice(), 1060)).toBe(false);
    expect(a.call(voice({ active: false }), 2000)).toBe(true);
    expect(a.radios(radios, 2000)).toEqual({});
    expect(a.repeaters(repeaters, 2000)).toEqual({});
    expect(a.active(2000)).toEqual([]);
    expect(a.next(2000)).toBeNull();
  });

  it('ends a call whose end frame never arrives after the timeout', () => {
    const a = new TxActivity();
    a.call(voice(), 1000);
    a.call(voice(), 2000);
    expect(a.next(2000)).toBe(2000 + CALL_TIMEOUT_MS);
    expect(a.expire(2000 + CALL_TIMEOUT_MS - 1)).toBe(false);
    expect(a.radios(radios, 2000 + CALL_TIMEOUT_MS - 1)).toEqual({ [RADIO]: 'loop' });
    expect(a.expire(2000 + CALL_TIMEOUT_MS)).toBe(true);
    expect(a.radios(radios, 2000 + CALL_TIMEOUT_MS)).toEqual({});
    expect(a.active(2000 + CALL_TIMEOUT_MS)).toEqual([]);
  });

  it('ignores a late active frame of a call that already ended', () => {
    const a = new TxActivity();
    a.call(voice(), 1000);
    a.call(voice({ active: false }), 2000);
    expect(a.call(voice(), 2010)).toBe(false);
    expect(a.radios(radios, 2010)).toEqual({});
  });

  it('gives a data call one short blip at the sender', () => {
    const a = new TxActivity();
    expect(a.call(data(), 1000)).toBe(true);
    expect(a.radios(radios, 1000)).toEqual({ [RADIO]: 'burst' });
    expect(a.repeaters(repeaters, 1000)).toEqual({ [REPEATER]: 'burst' });
    expect(a.call(data({ active: true }), 1100)).toBe(false);
    expect(a.next(1000)).toBe(1000 + BURST_MS);
    expect(a.expire(1000 + BURST_MS)).toBe(true);
    expect(a.radios(radios, 1000 + BURST_MS)).toEqual({});
    expect(a.call(data(), 1000 + BURST_MS + 1)).toBe(false);
  });

  it('keeps the line of a data call, then rings its receivers when the dot arrives', () => {
    const a = new TxActivity();
    a.call(data(), 1000, [{ kind: 'radio', id: OTHER }]);
    expect(a.active(1000).map((c) => [c.frame.id, c.data])).toEqual([[8, true]]);
    expect(a.radios(radios, 1000)).toEqual({ [RADIO]: 'burst' });
    expect(a.next(1000)).toBe(1000 + DOT_MS);
    expect(a.expire(1000 + DOT_MS)).toBe(true);
    expect(a.radios(radios, 1000 + DOT_MS)).toEqual({ [RADIO]: 'burst', [OTHER]: 'burst' });
    expect(a.expire(1000 + BURST_MS)).toBe(true);
    expect(a.radios(radios, 1000 + BURST_MS)).toEqual({ [OTHER]: 'burst' });
    expect(a.expire(1000 + DATA_LINE_MS)).toBe(true);
    expect(a.active(1000 + DATA_LINE_MS)).toEqual([]);
    expect(a.expire(1000 + DOT_MS + BURST_MS)).toBe(true);
    expect(a.radios(radios, 1000 + DOT_MS + BURST_MS)).toEqual({});
    expect(a.next(1000 + DOT_MS + BURST_MS)).toBeNull();
  });

  it('treats every kind of data call alike, whatever its subtype', () => {
    for (const over of [{}, { sub_kind: 'lrrp' }, { sub_kind: '' }, { kind: 'gps', sub_kind: '' }, { kind: 'csbk' }]) {
      const a = new TxActivity();
      const f = data(over);
      expect(isDataFrame(f)).toBe(true);
      expect(a.call(f, 1000, [{ kind: 'radio', id: OTHER }])).toBe(true);
      expect(a.active(1000).map((c) => c.data)).toEqual([true]);
      expect(a.radios(radios, 1000 + DOT_MS)).toEqual({ [RADIO]: 'burst', [OTHER]: 'burst' });
    }
  });

  it('gives a position report one short blip', () => {
    const a = new TxActivity();
    expect(a.position(RADIO, REPEATER, '2026-11-07T12:00:00Z', 1000)).toBe(true);
    expect(a.position(RADIO, REPEATER, '2026-11-07T12:00:00Z', 1001)).toBe(false);
    expect(a.radios(radios, 1001)).toEqual({ [RADIO]: 'burst' });
    expect(a.repeaters(repeaters, 1001)).toEqual({ [REPEATER]: 'burst' });
    expect(a.active(1001)).toEqual([]);
  });

  it('keeps the loop when a burst arrives during a call', () => {
    const a = new TxActivity();
    a.call(voice(), 1000);
    a.call(data({ id: 9 }), 1100);
    expect(a.radios(radios, 1100)).toEqual({ [RADIO]: 'loop' });
  });

  it('blips only the radios and repeaters on the map', () => {
    const a = new TxActivity();
    a.call(voice({ source_id: 3108888, repeater_id: 310888801 }), 1000);
    expect(a.radios(radios, 1000)).toEqual({});
    expect(a.repeaters(repeaters, 1000)).toEqual({});

    a.call(voice({ id: 10, source_id: 3108888 }), 1000);
    expect(a.radios(radios, 1000)).toEqual({});
    expect(a.repeaters(repeaters, 1000)).toEqual({ [REPEATER]: 'loop' });
    expect(a.radios(new Set(), 1000)).toEqual({});
  });

  it('falls back to the user of the call when the row has no source', () => {
    const a = new TxActivity();
    a.call(voice({ source_id: 0, user: { id: RADIO } }), 1000);
    expect(a.radios(radios, 1000)).toEqual({ [RADIO]: 'loop' });
  });

  it('ignores idle and other control traffic', () => {
    const a = new TxActivity();
    for (const kind of ['idle', 'usbd', 'mbc']) {
      expect(a.call(voice({ kind }), 1000)).toBe(false);
    }
    expect(a.radios(radios, 1000)).toEqual({});
  });

  it('reads only call rows from the socket', () => {
    expect(isTxFrame(voice())).toBe(true);
    expect(isTxFrame({ kind: 'position' })).toBe(false);
    expect(isTxFrame(null)).toBe(false);
    expect(isTxFrame('PONG')).toBe(false);
  });
});
