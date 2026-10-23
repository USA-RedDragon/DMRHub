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

import API from '@/services/API';
import type { BadgeTone } from '@/lib/badge';

export const MESSAGE_BOX_CAP = 250;

export type MessageFormat = 'tms' | 'ip' | 'anytone' | 'defined_data' | 'hytera' | 'data' | 'unknown';

export type MessageResponse = 'none' | 'ack' | 'nack' | 'sack';

export type MessageBox = 'inbox' | 'outbox';

export type Message = {
  id: number;
  received_at: string;
  source_id: number;
  source_callsign: string;
  destination_id: number;
  destination_callsign: string;
  group: boolean;
  talkgroup_name: string;
  format: MessageFormat;
  text: string | null;
  confirmed: boolean;
  response: MessageResponse;
  checksum_ok: boolean;
  octets: number;
};

export type MessagePage<T extends Message = Message> = {
  total: number;
  messages: T[];
};

export type MessageStats = {
  inbox: number;
  outbox: number;
  ack: number;
  nack: number;
  by_format: Partial<Record<MessageFormat, number>>;
  by_day: { day: string; count: number }[];
};

export type MessageEvent = {
  boxes: MessageBox[];
  message: Message;
};

export const FORMAT_LABELS: Record<MessageFormat, string> = {
  ip: 'ETSI IP',
  tms: 'Motorola TMS',
  anytone: 'AnyTone',
  hytera: 'Hytera',
  defined_data: 'Defined Data',
  data: 'Data (not text)',
  unknown: 'Unknown',
};

export const FORMAT_ORDER: MessageFormat[] = ['ip', 'tms', 'anytone', 'hytera', 'defined_data', 'data', 'unknown'];

export function formatLabel(format: string): string {
  return FORMAT_LABELS[format as MessageFormat] ?? format;
}

export function messageState(m: Pick<Message, 'confirmed' | 'response'>): { label: string; tone: BadgeTone } {
  if (!m.confirmed) {
    return { label: 'Unconfirmed', tone: 'none' };
  }
  switch (m.response) {
  case 'ack':
    return { label: 'Delivered (ACK)', tone: 'ok' };
  case 'nack':
    return { label: 'Rejected (NACK)', tone: 'bad' };
  case 'sack':
    return { label: 'Partial (SACK)', tone: 'warn' };
  default:
    return { label: 'Confirmed, no reply', tone: 'warn' };
  }
}

export function notTextLabel(m: Pick<Message, 'octets'>): string {
  return `Not text: ${m.octets} octets of data`;
}

export function boxPageCount(total: number, limit: number): number {
  const capped = Math.min(Math.max(total, 0), MESSAGE_BOX_CAP);
  return Math.max(1, Math.ceil(capped / limit));
}

export async function getBox(box: MessageBox, page = 1, limit = 25): Promise<MessagePage> {
  const res = await API.get(`/messages/${box}?page=${page}&limit=${limit}`);
  return {
    total: Math.min(res.data.total ?? 0, MESSAGE_BOX_CAP),
    messages: res.data.messages ?? [],
  };
}

export async function getMessage(id: number): Promise<Message> {
  const res = await API.get(`/messages/${id}`);
  return res.data;
}

export function localTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

export async function getStats(tz = localTimeZone()): Promise<MessageStats> {
  const res = await API.get(`/messages/stats?tz=${encodeURIComponent(tz)}`);
  return res.data;
}

/**
 * applyMessageEvent returns the rows of one page of a box after a websocket event.
 * A message with a known id replaces its row. A message with an id above newestId is new:
 * it goes on top of the first page, and the page keeps at most limit rows.
 * Any other message is a change to a row on another page, so the rows stay as they are.
 */
export function applyMessageEvent<T extends Message>(
  rows: T[],
  event: MessageEvent,
  box: MessageBox,
  page: number,
  limit: number,
  newestId: number,
): { rows: T[]; added: boolean } {
  if (!event.boxes.includes(box)) {
    return { rows, added: false };
  }
  const index = rows.findIndex((r) => r.id === event.message.id);
  if (index >= 0) {
    const next = rows.slice();
    next[index] = { ...next[index], ...event.message } as T;
    return { rows: next, added: false };
  }
  if (event.message.id <= newestId) {
    return { rows, added: false };
  }
  if (page !== 1) {
    return { rows, added: true };
  }
  return { rows: [event.message as T, ...rows].slice(0, limit), added: true };
}

export type MessageTransport = 'mmdvm' | 'ipsc' | 'openbridge';

export type AdminMessage = Message & {
  transmission_id: number | null;
  repeater_id: number;
  slot: number;
  transport: MessageTransport | string;
  stream_id: number;
  sap: number | null;
  bad_crc: boolean;
  blocks: number;
};

export type AdminMessageDetail = AdminMessage & {
  raw: string;
};

export type AdminMessageFilters = {
  src?: string;
  dst?: string;
  format?: string;
  from?: string;
  to?: string;
  text?: string;
};

const TRANSPORT_LABELS: Record<string, string> = {
  mmdvm: 'MMDVM',
  ipsc: 'IPSC',
  openbridge: 'OpenBridge',
};

export function transportLabel(transport: string): string {
  return TRANSPORT_LABELS[transport] ?? transport;
}

const SAP_NAMES: Record<number, string> = {
  0: 'UDT',
  2: 'TCP/IP header compression',
  3: 'UDP/IP header compression',
  4: 'IP',
  5: 'ARP',
  9: 'Proprietary',
  10: 'Short data',
};

export function sapLabel(sap: number | null | undefined): string {
  if (sap === null || sap === undefined) {
    return 'None';
  }
  const name = SAP_NAMES[sap];
  return name ? `${name} (${sap})` : `SAP ${sap}`;
}

/** hexDump formats a hex string as lines of 16 octets, each line led by its offset. */
export function hexDump(hex: string): string {
  const clean = hex.replace(/[^0-9a-fA-F]/g, '').toLowerCase();
  const octets: string[] = [];
  for (let i = 0; i + 1 < clean.length; i += 2) {
    octets.push(clean.slice(i, i + 2));
  }
  const lines: string[] = [];
  for (let i = 0; i < octets.length; i += 16) {
    lines.push(`${i.toString(16).padStart(4, '0')}  ${octets.slice(i, i + 16).join(' ')}`);
  }
  return lines.join('\n');
}

/** dayStart returns the RFC 3339 time of the local midnight that starts a yyyy-mm-dd day, moved by extraDays. */
export function dayStart(day: string, extraDays = 0): string | undefined {
  const [y, m, d] = day.split('-').map(Number);
  if (!y || !m || !d) {
    return undefined;
  }
  return new Date(y, m - 1, d + extraDays).toISOString();
}

export function adminSearchQuery(filters: AdminMessageFilters, page: number, limit: number): string {
  const query = new URLSearchParams();
  const src = filters.src?.trim();
  const dst = filters.dst?.trim();
  const text = filters.text?.trim();
  if (src) query.set('src', src);
  if (dst) query.set('dst', dst);
  if (filters.format) query.set('format', filters.format);
  const from = filters.from ? dayStart(filters.from) : undefined;
  const to = filters.to ? dayStart(filters.to, 1) : undefined;
  if (from) query.set('from', from);
  if (to) query.set('to', to);
  if (text) query.set('text', text);
  query.set('page', String(page));
  query.set('limit', String(limit));
  return query.toString();
}

export async function searchAdminMessages(
  filters: AdminMessageFilters,
  page = 1,
  limit = 25,
): Promise<MessagePage<AdminMessage>> {
  const res = await API.get(`/admin/messages?${adminSearchQuery(filters, page, limit)}`);
  return { total: res.data.total ?? 0, messages: res.data.messages ?? [] };
}

export async function getAdminMessage(id: number): Promise<AdminMessageDetail> {
  const res = await API.get(`/admin/messages/${id}`);
  return res.data;
}
