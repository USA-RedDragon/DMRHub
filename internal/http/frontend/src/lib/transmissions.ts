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

export type TypeKey = 'voice' | 'data' | 'sms' | 'gps' | 'alias' | 'csbk' | 'control' | 'unknown';

export const TYPES: { key: TypeKey; label: string }[] = [
  { key: 'voice', label: 'Voice' },
  { key: 'data', label: 'Data' },
  { key: 'sms', label: 'SMS' },
  { key: 'gps', label: 'GPS' },
  { key: 'alias', label: 'Talker Alias' },
  { key: 'csbk', label: 'CSBK' },
  { key: 'control', label: 'Control' },
  { key: 'unknown', label: 'Unknown' },
];

export const DEFAULT_TYPES: TypeKey[] = ['voice', 'data', 'sms', 'gps', 'alias', 'csbk', 'control'];

export type TypeFilterValue = {
  types: TypeKey[];
  idle: boolean;
};

export type CallRow = {
  id: number;
  active: boolean;
  start_time: string | Date;
  time_slot: boolean;
  group_call: boolean;
  repeater_id: number;
  repeater: { id: number; callsign: string };
  user: { id: number; callsign: string };
  is_to_talkgroup: boolean;
  to_talkgroup: { id: number };
  is_to_repeater: boolean;
  to_repeater: { id: number; callsign: string };
  is_to_user: boolean;
  to_user: { id: number; callsign: string };
  destination_id: number;
  duration: string | number;
  ber: string | number;
  loss: string | number;
  jitter: string | number;
  rssi: string | number;
  kind?: string;
  sub_kind?: string;
  source_id?: number;
  bursts?: number;
  fid?: number | null;
  opcode?: number | null;
  sap?: number | null;
  data_format?: string;
  confirmed?: boolean;
  blocks?: number;
  octets?: number;
  response?: string;
  talker_alias?: string;
  alias_format?: string;
  grid?: string;
  position_error?: number | null;
  partial?: boolean;
  undeliverable?: string[] | null;
};

const CONTROL_KINDS = ['udt', 'mbc', 'idle', 'usbd'];
const IDLE_KINDS = ['idle', 'usbd'];

const SAP_NAMES: Record<number, string> = {
  0: 'UDT',
  2: 'TCP/IP',
  3: 'UDP/IP',
  4: 'IP',
  5: 'ARP',
  9: 'Proprietary',
  10: 'Short data',
};

const SMS_FORMATS: Record<string, string> = {
  ip: 'ETSI IP',
  tms: 'Motorola TMS',
  anytone: 'AnyTone',
  hytera: 'Hytera',
  defined_data: 'Defined Data',
};

const DATA_SUBS: Record<string, string> = {
  lrrp: 'LRRP',
  ars: 'ARS',
  tms: 'TMS',
};

const CSBK_NAMES: Record<string, string> = {
  preamble: 'Preamble',
  radio_check: 'Radio Check',
  radio_check_answer: 'Radio Check Answer',
  call_alert: 'Call Alert',
  call_alert_ack: 'Call Alert ACK',
};

const UNKNOWN_NAMES: Record<string, string> = {
  bad_crc: 'Bad CRC',
  undecodable: 'Undecodable burst',
  reserved: 'Reserved data type',
  frame_type_3: 'Frame type 3',
};

const UNDELIVERABLE_REASONS: Record<string, (link: string) => string> = {
  invalid_data_type: () => 'the burst has an invalid data type',
  no_ipsc_form: (link) => `the ${link} cannot carry this kind of burst`,
  frame_type_3: () => 'the burst has an unknown frame type',
  fec_uncorrectable: () => 'the burst was too damaged to repair',
  slot_type: (link) => `the burst could not be rebuilt for the ${link}`,
  data_in_voice: () => 'a data burst came in the middle of a voice call',
  undecodable: () => 'the burst could not be read',
  stream_id_range: (link) => `the call ID does not fit the ${link}`,
};

const SERVER_NAMES: Record<string, string> = {
  ipsc: 'IPSC',
  mmdvm: 'MMDVM',
};

/** humanize turns a sub kind such as c_bcast into C bcast for display. */
export function humanize(sub: string): string {
  const text = sub.replace(/_/g, ' ').trim();
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export type UndeliverableLabel = {
  target: string;
  reason: string;
};

function repeaterCallsign(row: CallRow, id: number): string {
  if (row.is_to_repeater && row.to_repeater?.id === id && row.to_repeater.callsign) {
    return row.to_repeater.callsign;
  }
  if (row.repeater?.id === id && row.repeater.callsign) {
    return row.repeater.callsign;
  }
  return '';
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * undeliverableLabel words a last heard undeliverable target such as
 * ipsc:311304200:no_ipsc_form as the repeater it names and why it was not sent.
 */
export function undeliverableLabel(target: string, row: CallRow): UndeliverableLabel {
  const [server = '', id = '', ...rest] = target.split(':');
  const reason = rest.join(':');
  const serverName = SERVER_NAMES[server] || '';
  const kind = serverName ? `${serverName} repeater` : 'repeater';
  let who: string;
  if (!id) {
    who = serverName ? `an ${kind}` : `a ${kind}`;
  } else {
    const callsign = /^\d+$/.test(id) ? repeaterCallsign(row, Number(id)) : '';
    who = callsign ? `${kind} ${callsign} (${id})` : `${kind} ${id}`;
  }
  const link = serverName ? `${serverName} link` : 'link';
  const why = UNDELIVERABLE_REASONS[reason]?.(link) ?? `the burst could not be prepared for the ${link}`;
  return { target: who, reason: why };
}

/** undeliverableLines words a call's undeliverable list as lines to show under its type. */
export function undeliverableLines(row: CallRow): string[] {
  const labels = (row.undeliverable || []).map((t) => undeliverableLabel(t, row));
  if (labels.length === 1) {
    return [`Not sent to ${labels[0]!.target}`, capitalize(labels[0]!.reason)];
  }
  if (labels.length > 1) {
    return [`Not sent to ${labels.length} repeaters`, ...labels.map((l) => `${capitalize(l.target)}: ${l.reason}`)];
  }
  return [];
}

const POSITION_ERRORS: Record<number, string> = {
  0: '±2 m',
  1: '±20 m',
  2: '±200 m',
  3: '±2 km',
  4: '±20 km',
  5: '±200 km',
  6: '>200 km',
};

const MOTOROLA_FID = 0x10;

export function hex(n: number): string {
  return `0x${n.toString(16).toUpperCase().padStart(2, '0')}`;
}

export function typeOf(row: CallRow): TypeKey {
  const kind = row.kind || 'voice';
  if (kind === 'data') {
    return row.sub_kind === 'sms' ? 'sms' : 'data';
  }
  if (CONTROL_KINDS.includes(kind)) {
    return 'control';
  }
  if (kind === 'voice' || kind === 'gps' || kind === 'alias' || kind === 'csbk') {
    return kind;
  }
  return 'unknown';
}

export function typeLabel(key: TypeKey): string {
  return TYPES.find((t) => t.key === key)?.label ?? 'Unknown';
}

function plural(n: number, one: string): string {
  return `${n} ${one}${n === 1 ? '' : 's'}`;
}

function sizeText(row: CallRow): string {
  const blocks = row.blocks ?? 0;
  const octets = row.octets ?? 0;
  return `${plural(blocks, 'block')}, ${octets} B`;
}

function responseName(row: CallRow): string {
  switch (row.response) {
    case 'ack':
      return 'ACK';
    case 'nack':
      return 'NACK';
    case 'sack':
      return 'SACK';
    default:
      return 'no reply';
  }
}

function hasSize(row: CallRow): boolean {
  return (row.blocks ?? 0) > 0 || (row.octets ?? 0) > 0;
}

function smsLines(row: CallRow): string[] {
  const sap = row.sap ?? null;
  const format = SMS_FORMATS[row.data_format || ''] || 'Text';
  const sub = sap === null ? format : `${format} · SAP ${SAP_NAMES[sap] || 'SAP'} (${sap})`;
  const resp = row.confirmed ? `Confirmed, ${responseName(row)}` : 'Unconfirmed';
  return [sub, `${resp}, ${sizeText(row)}`];
}

function dataLines(row: CallRow): string[] {
  if (row.sub_kind === 'response') {
    return ['Response', responseName(row)];
  }
  const lines: string[] = [];
  const sap = row.sap ?? null;
  if (sap !== null) {
    let sub = `SAP ${sap} ${SAP_NAMES[sap] || ''}`.trim();
    const extra = DATA_SUBS[row.sub_kind || ''];
    if (extra) {
      sub += ` · ${extra}`;
    }
    lines.push(sub);
  }
  if (hasSize(row) || row.confirmed) {
    let subx = `${row.confirmed ? 'Confirmed' : 'Unconfirmed'}, ${sizeText(row)}`;
    if (row.confirmed) {
      subx += `, ${responseName(row)}`;
    }
    lines.push(subx);
  }
  return lines;
}

function csbkLines(row: CallRow): string[] {
  const sub = row.sub_kind || '';
  const lines = [CSBK_NAMES[sub] || humanize(sub)];
  if (sub === 'preamble') {
    lines.push(plural(row.bursts ?? 1, 'burst'));
  } else if (row.fid === MOTOROLA_FID) {
    lines.push(`Motorola FID ${hex(MOTOROLA_FID)}${row.response === 'ack' ? ', ACK' : ''}`);
  }
  return lines.filter((l) => l !== '');
}

function controlLines(row: CallRow): string[] {
  switch (row.kind) {
    case 'udt':
      return ['UDT', sizeText(row)];
    case 'mbc': {
      const opcode = row.opcode ?? null;
      const detail = opcode === null ? '' : `Opcode ${hex(opcode)} ${humanize(row.sub_kind || '')}`.trim();
      return detail ? ['MBC', detail] : ['MBC'];
    }
    case 'idle':
      return ['Idle', 'PR fill'];
    case 'usbd':
      return row.sub_kind ? ['USBD', humanize(row.sub_kind)] : ['USBD'];
  }
  return [];
}

function unknownLines(row: CallRow): string[] {
  if (row.sub_kind === 'csbk') {
    const opcode = row.opcode ?? 0;
    const fid = row.fid ?? 0;
    return [`CSBK opcode ${hex(opcode)}`, `FID ${hex(fid)}, forwarded unchanged`];
  }
  return [UNKNOWN_NAMES[row.sub_kind || ''] || 'Unknown burst', 'Forwarded unchanged'];
}

function gpsLines(row: CallRow): string[] {
  if (row.partial) {
    return ['Partial position'];
  }
  const parts: string[] = [];
  if (row.grid) {
    parts.push(row.grid);
  }
  const err = row.position_error ?? null;
  if (err !== null && POSITION_ERRORS[err]) {
    parts.push(POSITION_ERRORS[err]);
  }
  return parts.length ? [parts.join(' · ')] : [];
}

function aliasLines(row: CallRow): string[] {
  if (row.partial) {
    return ['Partial alias'];
  }
  const lines: string[] = [];
  if (row.talker_alias) {
    lines.push(row.talker_alias);
  }
  if (row.alias_format) {
    lines.push(`Format ${row.alias_format}`);
  }
  return lines;
}

export function typeLines(row: CallRow): string[] {
  let lines: string[];
  switch (typeOf(row)) {
    case 'voice':
      lines = row.sub_kind === 'encrypted' ? ['Encrypted'] : [];
      break;
    case 'sms':
      lines = smsLines(row);
      break;
    case 'data':
      lines = dataLines(row);
      break;
    case 'csbk':
      lines = csbkLines(row);
      break;
    case 'control':
      lines = controlLines(row);
      break;
    case 'gps':
      lines = gpsLines(row);
      break;
    case 'alias':
      lines = aliasLines(row);
      break;
    default:
      lines = unknownLines(row);
  }
  return lines;
}

export function isVoice(row: CallRow): boolean {
  return typeOf(row) === 'voice';
}

export function matchesFilter(row: CallRow, filter: TypeFilterValue): boolean {
  if (!filter.types.includes(typeOf(row))) {
    return false;
  }
  return filter.idle || !IDLE_KINDS.includes(row.kind || '');
}

export function filterQuery(filter: TypeFilterValue): string {
  let q = `types=${filter.types.join(',')}`;
  if (filter.idle) {
    q += '&idle=true';
  }
  return q;
}

export function defaultFilter(): TypeFilterValue {
  return { types: [...DEFAULT_TYPES], idle: false };
}

export function cleanCalls(data: Record<string, unknown>[] | null | undefined): CallRow[] {
  if (!data) {
    return [];
  }
  const rows = JSON.parse(JSON.stringify(data));
  for (const row of rows) {
    row.start_time = new Date(row.start_time);
    if (typeof row.duration == 'number') {
      row.duration = (row.duration / 1000000000).toFixed(1);
    }
    if (typeof row.loss == 'number') {
      row.loss = (row.loss * 100).toFixed(1);
    }
    if (typeof row.ber == 'number') {
      row.ber = (row.ber * 100).toFixed(1);
    }
    if (typeof row.jitter == 'number') {
      row.jitter = row.jitter.toFixed(1);
    }
    if (typeof row.rssi == 'number') {
      row.rssi = row.rssi.toFixed(0);
    }
  }
  return rows;
}

export function durationText(row: CallRow): string {
  if (isVoice(row)) {
    return `${row.duration}s`;
  }
  return Number(row.duration) > 0 ? `${row.duration}s` : '-';
}

export function berText(row: CallRow): string {
  return isVoice(row) ? `${row.ber}%` : '-';
}

export function lossText(row: CallRow): string {
  return isVoice(row) ? `${row.loss}%` : '-';
}

export function jitterText(row: CallRow): string {
  return isVoice(row) ? `${row.jitter}ms` : '-';
}

export function rssiText(row: CallRow): string {
  const rssi = Number(row.rssi);
  return isVoice(row) && rssi !== 0 ? `-${rssi}dBm` : '-';
}

export const TYPE_COLORS: Record<TypeKey, { badge: string; chip: string; dot: string }> = {
  voice: { badge: 'bg-blue-500/10 text-blue-500', chip: 'border-blue-500/45 bg-blue-500/10', dot: 'bg-blue-500' },
  data: { badge: 'bg-cyan-500/10 text-cyan-500', chip: 'border-cyan-500/45 bg-cyan-500/10', dot: 'bg-cyan-500' },
  sms: { badge: 'bg-green-500/10 text-green-500', chip: 'border-green-500/45 bg-green-500/10', dot: 'bg-green-500' },
  gps: { badge: 'bg-teal-500/10 text-teal-500', chip: 'border-teal-500/45 bg-teal-500/10', dot: 'bg-teal-500' },
  alias: { badge: 'bg-purple-500/10 text-purple-500', chip: 'border-purple-500/45 bg-purple-500/10', dot: 'bg-purple-500' },
  csbk: { badge: 'bg-orange-500/10 text-orange-500', chip: 'border-orange-500/45 bg-orange-500/10', dot: 'bg-orange-500' },
  control: { badge: 'bg-indigo-500/10 text-indigo-500', chip: 'border-indigo-500/45 bg-indigo-500/10', dot: 'bg-indigo-500' },
  unknown: { badge: 'bg-zinc-500/10 text-zinc-500', chip: 'border-zinc-500/45 bg-zinc-500/10', dot: 'bg-zinc-500' },
};
