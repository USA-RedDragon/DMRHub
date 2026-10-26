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
  undeliverableLabel,
  undeliverableLines,
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
    expect(typeLines(row({ kind: 'mbc', opcode: 0x28, sub_kind: 'c_bcast' }))).toEqual(['MBC', 'Opcode 0x28 C bcast']);
    expect(typeLines(row({ kind: 'idle' }))).toEqual(['Idle', 'PR fill']);
    expect(typeLines(row({ kind: 'usbd', sub_kind: 'location_information_protocol' }))).toEqual(['USBD', 'Location information protocol']);
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
    expect(typeLines(row({ kind: 'idle', undeliverable: ['ipsc:311304200:no_ipsc_form'] }))).toEqual(['Idle', 'PR fill']);
  });
});

describe('undeliverableLabel', () => {
  const r = row({});
  const ipsc = 'IPSC repeater 311304200';

  it('words each known IPSC reason', () => {
    expect(undeliverableLabel('ipsc:311304200:no_ipsc_form', r)).toEqual({
      target: ipsc,
      reason: 'the IPSC link cannot carry this kind of burst',
    });
    expect(undeliverableLabel('ipsc:311304200:invalid_data_type', r).reason).toBe('the burst has an invalid data type');
    expect(undeliverableLabel('ipsc:311304200:frame_type_3', r).reason).toBe('the burst has an unknown frame type');
    expect(undeliverableLabel('ipsc:311304200:fec_uncorrectable', r).reason).toBe('the burst was too damaged to repair');
    expect(undeliverableLabel('ipsc:311304200:slot_type', r).reason).toBe('the burst could not be rebuilt for the IPSC link');
    expect(undeliverableLabel('ipsc:311304200:data_in_voice', r).reason).toBe('a data burst came in the middle of a voice call');
    expect(undeliverableLabel('ipsc:311304200:slot_busy', r).reason).toBe('the timeslot stayed busy with other calls for 30 seconds');
    expect(undeliverableLabel('ipsc:311304200:undecodable', r).reason).toBe('the burst could not be read');
    expect(undeliverableLabel('ipsc:311304200:stream_id_range', r).reason).toBe('the call ID does not fit the IPSC link');
  });

  it('gives an unknown reason a generic wording without the raw code', () => {
    expect(undeliverableLabel('ipsc:311304200:encode_failed', r)).toEqual({
      target: ipsc,
      reason: 'the burst could not be prepared for the IPSC link',
    });
  });

  it('names the repeater when the row already has its callsign', () => {
    const withName = row({ is_to_repeater: true, to_repeater: { id: 311304200, callsign: 'W1XPR' } });
    expect(undeliverableLabel('ipsc:311304200:no_ipsc_form', withName).target).toBe('IPSC repeater W1XPR (311304200)');
    const source = row({ repeater: { id: 311304200, callsign: 'W1XPR' } });
    expect(undeliverableLabel('ipsc:311304200:no_ipsc_form', source).target).toBe('IPSC repeater W1XPR (311304200)');
  });

  it('copes with entries that miss parts', () => {
    expect(undeliverableLabel('ipsc:311304200', r)).toEqual({
      target: ipsc,
      reason: 'the burst could not be prepared for the IPSC link',
    });
    expect(undeliverableLabel('ipsc', r).target).toBe('an IPSC repeater');
    expect(undeliverableLabel('ipsc::no_ipsc_form', r)).toEqual({
      target: 'an IPSC repeater',
      reason: 'the IPSC link cannot carry this kind of burst',
    });
    expect(undeliverableLabel('', r)).toEqual({
      target: 'a repeater',
      reason: 'the burst could not be prepared for the link',
    });
  });

  it('handles a target that is not IPSC', () => {
    expect(undeliverableLabel('mmdvm:311000:build_failed', r)).toEqual({
      target: 'MMDVM repeater 311000',
      reason: 'the burst could not be prepared for the MMDVM link',
    });
    expect(undeliverableLabel('newproto:42:build_failed', r)).toEqual({
      target: 'repeater 42',
      reason: 'the burst could not be prepared for the link',
    });
  });
});

describe('undeliverableLines', () => {
  it('is empty when every target took the call', () => {
    expect(undeliverableLines(row({}))).toEqual([]);
    expect(undeliverableLines(row({ undeliverable: null }))).toEqual([]);
    expect(undeliverableLines(row({ undeliverable: [] }))).toEqual([]);
  });

  it('shows a single target with its reason on the next line', () => {
    expect(undeliverableLines(row({ undeliverable: ['ipsc:311304200:no_ipsc_form'] }))).toEqual([
      'Not sent to IPSC repeater 311304200',
      'The IPSC link cannot carry this kind of burst',
    ]);
  });

  it('counts several targets, then gives each with its reason', () => {
    expect(undeliverableLines(row({ undeliverable: ['ipsc:311304200:no_ipsc_form', 'ipsc::fec_uncorrectable'] }))).toEqual([
      'Not sent to 2 repeaters',
      'IPSC repeater 311304200: the IPSC link cannot carry this kind of burst',
      'An IPSC repeater: the burst was too damaged to repair',
    ]);
  });
});

describe('undeliverable sentences', () => {
  const pairs: [string, string][] = [
    ['ipsc:314926:invalid_data_type', 'The burst for IPSC repeater 314926 had an invalid data type.'],
    ['ipsc:314926:no_ipsc_form', 'IPSC repeater 314926 cannot carry this kind of burst.'],
    ['ipsc:314926:frame_type_3', 'The burst for IPSC repeater 314926 had an unknown frame type.'],
    ['ipsc:314926:fec_uncorrectable', 'The burst for IPSC repeater 314926 was too damaged to repair.'],
    ['ipsc:314926:slot_type', 'The burst could not be rebuilt for IPSC repeater 314926.'],
    ['ipsc:314926:stream_id_range', 'The call ID does not fit the link to IPSC repeater 314926.'],
    ['ipsc:314926:build_failed', 'The burst could not be built for IPSC repeater 314926.'],
    ['ipsc:314926:encode_failed', 'The burst could not be encoded for IPSC repeater 314926.'],
    ['ipsc:314926:data_in_voice', 'A data burst for IPSC repeater 314926 came in the middle of a voice call.'],
    ['ipsc:314926:undecodable', 'The burst for IPSC repeater 314926 could not be read.'],
    ['ipsc:314926:slot_busy', 'IPSC repeater 314926 was busy on that slot.'],
    ['ipsc:314926:', 'The burst could not be prepared for IPSC repeater 314926.'],
    ['ipsc::slot_busy', 'An IPSC repeater was busy on that slot.'],
    ['mmdvm:311000:build_failed', 'The burst could not be built for MMDVM repeater 311000.'],
    ['openbridge:311000:slot_busy', 'OpenBridge peer 311000 was busy on that slot.'],
    ['newproto:42:slot_busy', 'Repeater 42 was busy on that slot.'],
    [':', 'The burst could not be prepared for a repeater.'],
  ];

  it('shows a stored sentence as it showed the old entry', () => {
    const named = row({ is_to_repeater: true, to_repeater: { id: 314926, callsign: 'W1XPR' } });
    for (const [old, sentence] of pairs) {
      expect({ sentence, label: undeliverableLabel(sentence, named) }).toEqual({ sentence, label: undeliverableLabel(old, named) });
    }
  });

  it('shows the lines of a call with sentences as with old entries', () => {
    const named = row({ repeater: { id: 314926, callsign: 'W1XPR' } });
    expect(undeliverableLines({ ...named, undeliverable: ['IPSC repeater 314926 was busy on that slot.'] })).toEqual([
      'Not sent to IPSC repeater W1XPR (314926)',
      'The timeslot stayed busy with other calls for 30 seconds',
    ]);
    const both = { ...named, undeliverable: pairs.slice(0, 2).map(([, s]) => s) };
    expect(undeliverableLines(both)).toEqual(undeliverableLines({ ...named, undeliverable: pairs.slice(0, 2).map(([o]) => o) }));
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
