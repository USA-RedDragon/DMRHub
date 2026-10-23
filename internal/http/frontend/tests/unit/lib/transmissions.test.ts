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
  berText,
  defaultFilter,
  durationText,
  filterQuery,
  matchesFilter,
  rssiText,
  typeLines,
  typeOf,
  type CallRow,
} from '@/lib/transmissions';

function row(fields: Partial<CallRow>): CallRow {
  return {
    id: 1,
    active: false,
    start_time: new Date(),
    time_slot: false,
    group_call: true,
    repeater_id: 0,
    repeater: { id: 0, callsign: '' },
    user: { id: 3191868, callsign: 'KI5VMF' },
    is_to_talkgroup: true,
    to_talkgroup: { id: 3100 },
    is_to_repeater: false,
    to_repeater: { id: 0, callsign: '' },
    is_to_user: false,
    to_user: { id: 0, callsign: '' },
    destination_id: 3100,
    duration: '0.0',
    ber: '0.0',
    loss: '0.0',
    jitter: '0.0',
    rssi: '0',
    ...fields,
  };
}

describe('typeOf', () => {
  it('maps kinds to the filter types', () => {
    expect(typeOf(row({}))).toBe('voice');
    expect(typeOf(row({ kind: 'voice' }))).toBe('voice');
    expect(typeOf(row({ kind: 'data', sub_kind: 'sms' }))).toBe('sms');
    expect(typeOf(row({ kind: 'data', sub_kind: '' }))).toBe('data');
    expect(typeOf(row({ kind: 'udt' }))).toBe('control');
    expect(typeOf(row({ kind: 'mbc' }))).toBe('control');
    expect(typeOf(row({ kind: 'idle' }))).toBe('control');
    expect(typeOf(row({ kind: 'usbd' }))).toBe('control');
    expect(typeOf(row({ kind: 'gps' }))).toBe('gps');
    expect(typeOf(row({ kind: 'alias' }))).toBe('alias');
    expect(typeOf(row({ kind: 'csbk' }))).toBe('csbk');
    expect(typeOf(row({ kind: 'unknown' }))).toBe('unknown');
    expect(typeOf(row({ kind: 'something new' }))).toBe('unknown');
  });
});

describe('typeLines', () => {
  it('describes an SMS', () => {
    expect(typeLines(row({ kind: 'data', sub_kind: 'sms', data_format: 'ip', sap: 4, confirmed: true, response: 'ack', blocks: 3, octets: 44 })))
      .toEqual(['ETSI IP · SAP IP (4)', 'Confirmed, ACK, 3 blocks, 44 B']);
    expect(typeLines(row({ kind: 'data', sub_kind: 'sms', data_format: 'anytone', sap: 9, confirmed: false, blocks: 2, octets: 22 })))
      .toEqual(['AnyTone · SAP Proprietary (9)', 'Unconfirmed, 2 blocks, 22 B']);
  });

  it('shows a masked SMS as plain data without details', () => {
    expect(typeLines(row({ kind: 'data', sub_kind: '', sap: null, blocks: 0, octets: 0 }))).toEqual([]);
  });

  it('describes data', () => {
    expect(typeLines(row({ kind: 'data', sub_kind: 'lrrp', sap: 4, blocks: 2, octets: 24 })))
      .toEqual(['SAP 4 IP · LRRP', 'Unconfirmed, 2 blocks, 24 B']);
    expect(typeLines(row({ kind: 'data', sub_kind: 'ars', sap: 4, confirmed: true, response: 'ack', blocks: 3, octets: 40 })))
      .toEqual(['SAP 4 IP · ARS', 'Confirmed, 3 blocks, 40 B, ACK']);
    expect(typeLines(row({ kind: 'data', sub_kind: 'response', response: 'nack' }))).toEqual(['Response', 'NACK']);
  });

  it('describes control blocks', () => {
    expect(typeLines(row({ kind: 'csbk', sub_kind: 'preamble', bursts: 4 }))).toEqual(['Preamble', '4 bursts']);
    expect(typeLines(row({ kind: 'csbk', sub_kind: 'radio_check', fid: 16 }))).toEqual(['Radio Check', 'Motorola FID 0x10']);
    expect(typeLines(row({ kind: 'csbk', sub_kind: 'call_alert', fid: 16, response: 'ack' }))).toEqual(['Call Alert', 'Motorola FID 0x10, ACK']);
    expect(typeLines(row({ kind: 'udt', blocks: 1, octets: 12 }))).toEqual(['UDT', '1 block, 12 B']);
    expect(typeLines(row({ kind: 'mbc', opcode: 0x28, sub_kind: 'C_BCAST' }))).toEqual(['MBC', 'Opcode 0x28 C_BCAST']);
    expect(typeLines(row({ kind: 'idle' }))).toEqual(['Idle', 'PR fill']);
    expect(typeLines(row({ kind: 'usbd', sub_kind: '0' }))).toEqual(['USBD', 'Service 0']);
    expect(typeLines(row({ kind: 'unknown', sub_kind: 'csbk', opcode: 0x3a, fid: 0x68 })))
      .toEqual(['CSBK opcode 0x3A', 'FID 0x68, forwarded unchanged']);
  });

  it('describes GPS, alias and encrypted voice', () => {
    expect(typeLines(row({ kind: 'gps', grid: 'EM10', position_error: 0 }))).toEqual(['EM10 · ±2 m']);
    expect(typeLines(row({ kind: 'gps', partial: true }))).toEqual(['Partial position']);
    expect(typeLines(row({ kind: 'alias', talker_alias: 'KI5VMF Jacob', alias_format: '7-bit' }))).toEqual(['KI5VMF Jacob', 'Format 7-bit']);
    expect(typeLines(row({ kind: 'voice', sub_kind: 'encrypted' }))).toEqual(['Encrypted']);
    expect(typeLines(row({ kind: 'voice' }))).toEqual([]);
  });

  it('names the targets that could not take a burst', () => {
    expect(typeLines(row({ kind: 'idle', undeliverable: ['ipsc:311304200'] }))).toEqual(['Idle', 'PR fill', 'Not delivered: ipsc 311304200']);
  });
});

describe('filters', () => {
  it('hides unknown, idle and USBD by default', () => {
    const f = defaultFilter();
    expect(matchesFilter(row({ kind: 'voice' }), f)).toBe(true);
    expect(matchesFilter(row({ kind: 'unknown' }), f)).toBe(false);
    expect(matchesFilter(row({ kind: 'idle' }), f)).toBe(false);
    expect(matchesFilter(row({ kind: 'usbd' }), f)).toBe(false);
    expect(matchesFilter(row({ kind: 'mbc' }), f)).toBe(true);
    expect(matchesFilter(row({ kind: 'idle' }), { ...f, idle: true })).toBe(true);
    expect(filterQuery(f)).toBe('types=voice,data,sms,gps,alias,csbk,control');
    expect(filterQuery({ types: ['unknown'], idle: true })).toBe('types=unknown&idle=true');
  });
});

describe('cells', () => {
  it('shows voice quality only for voice', () => {
    expect(berText(row({ kind: 'voice', ber: '0.4' }))).toBe('0.4%');
    expect(berText(row({ kind: 'csbk', ber: '0.0' }))).toBe('-');
    expect(rssiText(row({ kind: 'voice', rssi: '47' }))).toBe('-47dBm');
    expect(rssiText(row({ kind: 'data', rssi: '47' }))).toBe('-');
    expect(durationText(row({ kind: 'csbk', duration: '0.0' }))).toBe('-');
    expect(durationText(row({ kind: 'data', duration: '0.6' }))).toBe('0.6s');
    expect(durationText(row({ kind: 'voice', duration: '0.0' }))).toBe('0.0s');
  });
});
