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
  undeliverableView,
  type CallRow,
  type UndeliverableEntry,
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
    expect(typeLines(row({ kind: 'idle', undeliverable: [entry(311304200, 'no_ipsc_form')], undeliverable_scope: 'all' }))).toEqual([
      'Idle',
      'PR fill',
    ]);
  });
});

function entry(id: number, reason: string, callsign = '', target = 'ipsc'): UndeliverableEntry {
  return { sentence: `sentence ${id} ${reason}`, target, repeater_id: id, callsign, reason };
}

describe('undeliverableView', () => {
  const none = { lines: [], summary: '', details: [] };

  it('is empty when every repeater took the call or the viewer may not see it', () => {
    expect(undeliverableView(row({}))).toEqual(none);
    expect(undeliverableView(row({ undeliverable: null }))).toEqual(none);
    expect(undeliverableView(row({ undeliverable: [], undeliverable_scope: '' }))).toEqual(none);
  });

  it('shows a single repeater on one line', () => {
    expect(undeliverableView(row({ undeliverable: [entry(314926, 'slot_busy', 'W1XPR')], undeliverable_scope: 'all' }))).toEqual({
      ...none,
      lines: ['Not sent to IPSC repeater W1XPR (314926): busy on that slot'],
    });
    expect(undeliverableView(row({ undeliverable: [entry(311304200, 'no_ipsc_form')], undeliverable_scope: 'all' })).lines).toEqual([
      'Not sent to IPSC repeater 311304200: could not carry this burst',
    ]);
  });

  it('words each reason code', () => {
    const words: Record<string, string> = {
      invalid_data_type: 'invalid data type',
      no_ipsc_form: 'could not carry this burst',
      frame_type_3: 'unknown frame type',
      fec_uncorrectable: 'too damaged to repair',
      slot_type: 'could not rebuild this burst',
      stream_id_range: 'call ID does not fit the link',
      build_failed: 'could not build this burst',
      encode_failed: 'could not encode this burst',
      data_in_voice: 'data burst in a voice call',
      undecodable: 'could not read this burst',
      slot_busy: 'busy on that slot',
      '': 'could not prepare this burst',
      new_code: 'could not prepare this burst',
    };
    for (const [reason, want] of Object.entries(words)) {
      const view = undeliverableView(row({ undeliverable: [entry(1, reason)], undeliverable_scope: 'all' }));
      expect(view.lines).toEqual([`Not sent to IPSC repeater 1: ${want}`]);
    }
  });

  it('names a repeater that the sentence does not name, and other targets', () => {
    const lines = (e: UndeliverableEntry) => undeliverableView(row({ undeliverable: [e], undeliverable_scope: 'all' })).lines;
    expect(lines(entry(0, 'slot_busy'))).toEqual(['Not sent to an IPSC repeater: busy on that slot']);
    expect(lines(entry(0, 'slot_busy', '', ''))).toEqual(['Not sent to a repeater: busy on that slot']);
    expect(lines(entry(311000, 'build_failed', 'W1RPT', 'mmdvm'))).toEqual(['Not sent to MMDVM repeater W1RPT (311000): could not build this burst']);
    expect(lines(entry(311000, 'slot_busy', '', 'openbridge'))).toEqual(['Not sent to OpenBridge peer 311000: busy on that slot']);
  });

  it('counts the repeaters by reason and lists them under the count', () => {
    const busy = [
      entry(314926, 'slot_busy', 'W1XPR'),
      entry(311304200, 'slot_busy', 'K5ABC'),
      entry(311304201, 'slot_busy', 'N0XYZ'),
      ...Array.from({ length: 35 }, (_, i) => entry(311307000 + i, 'slot_busy')),
    ];
    const undeliverable = [
      entry(311305510, 'no_ipsc_form'),
      ...busy.slice(0, 20),
      entry(311306001, 'fec_uncorrectable', 'KD5QRS'),
      ...busy.slice(20),
      entry(311305511, 'no_ipsc_form'),
    ];
    expect(undeliverableView(row({ undeliverable, undeliverable_scope: 'all' }))).toEqual({
      lines: ['Not sent to 41 repeaters', '38 busy on that slot · 2 could not carry this burst · 1 too damaged to repair'],
      summary: 'Show the 41 repeaters',
      details: [
        'Busy on that slot: W1XPR (314926), K5ABC (311304200), N0XYZ (311304201), and 35 more',
        'Could not carry this burst: 311305510, 311305511',
        'Too damaged to repair: KD5QRS (311306001)',
      ],
    });
  });

  it('counts a repeater with two reasons once', () => {
    const view = undeliverableView(row({
      undeliverable: [entry(314926, 'slot_busy', 'W1XPR'), entry(314926, 'fec_uncorrectable', 'W1XPR'), entry(311304200, 'slot_busy')],
      undeliverable_scope: 'all',
    }));
    expect(view.lines).toEqual(['Not sent to 2 repeaters', '2 busy on that slot · 1 too damaged to repair']);
    expect(view.summary).toBe('Show the 2 repeaters');
    const one = undeliverableView(row({
      undeliverable: [entry(314926, 'slot_busy', 'W1XPR'), entry(314926, 'fec_uncorrectable', 'W1XPR')],
      undeliverable_scope: 'all',
    }));
    expect(one).toEqual({
      ...none,
      lines: ['Not sent to IPSC repeater W1XPR (314926): busy on that slot', 'Not sent to IPSC repeater W1XPR (314926): too damaged to repair'],
    });
  });

  it('tells the owner of a repeater about their own repeater', () => {
    expect(undeliverableView(row({ undeliverable: [entry(314926, 'slot_busy', 'W1XPR')], undeliverable_scope: 'own' }))).toEqual({
      ...none,
      lines: ['Not sent to your repeater W1XPR (314926): busy on that slot'],
    });
    expect(undeliverableView(row({
      undeliverable: [entry(314926, 'slot_busy', 'W1XPR'), entry(314927, 'fec_uncorrectable'), entry(311000, 'slot_busy', '', 'openbridge')],
      undeliverable_scope: 'own',
    })).lines).toEqual([
      'Not sent to your repeater W1XPR (314926): busy on that slot',
      'Not sent to your repeater 314927: too damaged to repair',
      'Not sent to your OpenBridge peer 311000: busy on that slot',
    ]);
  });

  it('never shows the stored sentence or a reason code', () => {
    const view = undeliverableView(row({ undeliverable: [entry(1, 'encode_failed'), entry(2, 'slot_busy')], undeliverable_scope: 'all' }));
    const text = [...view.lines, view.summary, ...view.details].join('\n');
    expect(text).not.toContain('sentence');
    expect(text).not.toContain('encode_failed');
    expect(text).not.toContain('slot_busy');
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
