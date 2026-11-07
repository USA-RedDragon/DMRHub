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
import { BURST_MS, CALL_TIMEOUT_MS, isTxFrame, TxActivity, type TxFrame } from '@/lib/txActivity';

const RADIO = 3109999;
const REPEATER = 310999901;
const visibleRadios = new Set([RADIO]);
const visibleRepeaters = new Set([REPEATER]);

const voice = (over: Partial<TxFrame> = {}): TxFrame => ({
  id: 7,
  active: true,
  kind: 'voice',
  source_id: RADIO,
  repeater_id: REPEATER,
  ...over,
});

describe('TxActivity', () => {
  it('blips a radio and its repeater from call start until call end', () => {
    const a = new TxActivity();
    expect(a.call(voice(), 1000)).toBe(true);
    expect(a.radios(visibleRadios)).toEqual({ [RADIO]: 'loop' });
    expect(a.repeaters(visibleRepeaters)).toEqual({ [REPEATER]: 'loop' });
    expect(a.call(voice(), 1060)).toBe(false);
    expect(a.call(voice({ active: false }), 2000)).toBe(true);
    expect(a.radios(visibleRadios)).toEqual({});
    expect(a.repeaters(visibleRepeaters)).toEqual({});
    expect(a.next()).toBeNull();
  });

  it('ends a call whose end frame never arrives after the timeout', () => {
    const a = new TxActivity();
    a.call(voice(), 1000);
    a.call(voice(), 2000);
    expect(a.next()).toBe(2000 + CALL_TIMEOUT_MS);
    expect(a.expire(2000 + CALL_TIMEOUT_MS - 1)).toBe(false);
    expect(a.radios(visibleRadios)).toEqual({ [RADIO]: 'loop' });
    expect(a.expire(2000 + CALL_TIMEOUT_MS)).toBe(true);
    expect(a.radios(visibleRadios)).toEqual({});
  });

  it('ignores a late active frame of a call that already ended', () => {
    const a = new TxActivity();
    a.call(voice(), 1000);
    a.call(voice({ active: false }), 2000);
    expect(a.call(voice(), 2010)).toBe(false);
    expect(a.radios(visibleRadios)).toEqual({});
  });

  it('gives a data burst one short blip', () => {
    const a = new TxActivity();
    const sms = voice({ id: 8, kind: 'data', active: false });
    expect(a.call(sms, 1000)).toBe(true);
    expect(a.radios(visibleRadios)).toEqual({ [RADIO]: 'burst' });
    expect(a.repeaters(visibleRepeaters)).toEqual({ [REPEATER]: 'burst' });
    expect(a.call({ ...sms, active: true }, 1100)).toBe(false);
    expect(a.next()).toBe(1000 + BURST_MS);
    expect(a.expire(1000 + BURST_MS)).toBe(true);
    expect(a.radios(visibleRadios)).toEqual({});
    expect(a.call(sms, 1000 + BURST_MS + 1)).toBe(false);
  });

  it('gives a position report one short blip', () => {
    const a = new TxActivity();
    expect(a.position(RADIO, REPEATER, '2026-11-07T12:00:00Z', 1000)).toBe(true);
    expect(a.position(RADIO, REPEATER, '2026-11-07T12:00:00Z', 1001)).toBe(false);
    expect(a.radios(visibleRadios)).toEqual({ [RADIO]: 'burst' });
    expect(a.repeaters(visibleRepeaters)).toEqual({ [REPEATER]: 'burst' });
  });

  it('keeps the loop when a burst arrives during a call', () => {
    const a = new TxActivity();
    a.call(voice(), 1000);
    a.call(voice({ id: 9, kind: 'gps' }), 1100);
    expect(a.radios(visibleRadios)).toEqual({ [RADIO]: 'loop' });
  });

  it('blips only the radios and repeaters on the map', () => {
    const a = new TxActivity();
    a.call(voice({ source_id: 3108888, repeater_id: 310888801 }), 1000);
    expect(a.radios(visibleRadios)).toEqual({});
    expect(a.repeaters(visibleRepeaters)).toEqual({});

    a.call(voice({ id: 10, source_id: 3108888 }), 1000);
    expect(a.radios(visibleRadios)).toEqual({});
    expect(a.repeaters(visibleRepeaters)).toEqual({ [REPEATER]: 'loop' });
    expect(a.radios(new Set())).toEqual({});
  });

  it('falls back to the user of the call when the row has no source', () => {
    const a = new TxActivity();
    a.call(voice({ source_id: 0, user: { id: RADIO } }), 1000);
    expect(a.radios(visibleRadios)).toEqual({ [RADIO]: 'loop' });
  });

  it('ignores idle and other control traffic', () => {
    const a = new TxActivity();
    for (const kind of ['idle', 'usbd', 'mbc']) {
      expect(a.call(voice({ kind }), 1000)).toBe(false);
    }
    expect(a.radios(visibleRadios)).toEqual({});
  });

  it('reads only call rows from the socket', () => {
    expect(isTxFrame(voice())).toBe(true);
    expect(isTxFrame({ kind: 'position' })).toBe(false);
    expect(isTxFrame(null)).toBe(false);
    expect(isTxFrame('PONG')).toBe(false);
  });
});
