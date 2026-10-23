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

import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest';
import {
  adminSearchQuery,
  applyMessageEvent,
  dayStart,
  hexDump,
  sapLabel,
  transportLabel,
  boxPageCount,
  getBox,
  getStats,
  messageState,
  notTextLabel,
  type Message,
} from '@/services/messages';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
  },
}));

import API from '@/services/API';
const mockAPI = API as unknown as { get: Mock };

function msg(id: number, over: Partial<Message> = {}): Message {
  return {
    id,
    received_at: '2026-10-22T19:22:05Z',
    source_id: 3140598,
    source_callsign: 'KD5SMP',
    destination_id: 3191868,
    destination_callsign: 'KI5VMF',
    group: false,
    talkgroup_name: '',
    format: 'ip',
    text: 'TEST',
    confirmed: true,
    response: 'ack',
    checksum_ok: true,
    octets: 4,
    ...over,
  };
}

describe('messages service', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('asks for one page of a box', async () => {
    mockAPI.get.mockResolvedValue({ data: { total: 37, messages: [msg(1)] } });
    const page = await getBox('outbox', 2, 25);
    expect(mockAPI.get).toHaveBeenCalledWith('/messages/outbox?page=2&limit=25');
    expect(page.total).toBe(37);
    expect(page.messages).toHaveLength(1);
  });

  it('never reports more than 250 messages in a box', async () => {
    mockAPI.get.mockResolvedValue({ data: { total: 251, messages: [] } });
    expect((await getBox('inbox')).total).toBe(250);
    expect(boxPageCount(251, 25)).toBe(10);
    expect(boxPageCount(250, 50)).toBe(5);
    expect(boxPageCount(0, 25)).toBe(1);
  });

  it('sends the time zone for the stats', async () => {
    mockAPI.get.mockResolvedValue({ data: {} });
    await getStats('America/Chicago');
    expect(mockAPI.get).toHaveBeenCalledWith('/messages/stats?tz=America%2FChicago');
  });

  it('names the state of a message', () => {
    expect(messageState({ confirmed: false, response: 'none' })).toEqual({ label: 'Unconfirmed', tone: 'none' });
    expect(messageState({ confirmed: true, response: 'ack' }).label).toBe('Delivered (ACK)');
    expect(messageState({ confirmed: true, response: 'nack' }).label).toBe('Rejected (NACK)');
    expect(messageState({ confirmed: true, response: 'sack' }).label).toBe('Partial (SACK)');
    expect(messageState({ confirmed: true, response: 'none' }).label).toBe('Confirmed, no reply');
  });

  it('shows the size of a message that is not text when it has one', () => {
    expect(notTextLabel({ octets: 92 })).toBe('Not text: 92 octets of data');
    expect(notTextLabel({ octets: 0 })).toBe('Not text: 0 octets of data');
  });

  it('puts a new message on top of the first page and keeps the page size', () => {
    const rows = [msg(3), msg(2), msg(1)];
    const res = applyMessageEvent(rows, { boxes: ['inbox'], message: msg(4) }, 'inbox', 1, 3, 3);
    expect(res.added).toBe(true);
    expect(res.rows.map((r) => r.id)).toEqual([4, 3, 2]);
  });

  it('replaces a message that changed', () => {
    const rows = [msg(2, { response: 'none' }), msg(1)];
    const res = applyMessageEvent(rows, { boxes: ['outbox'], message: msg(2, { response: 'nack' }) }, 'outbox', 1, 25, 2);
    expect(res.added).toBe(false);
    expect(res.rows[0]!.response).toBe('nack');
    expect(res.rows).toHaveLength(2);
  });

  it('leaves a box alone when the message is not in it', () => {
    const rows = [msg(1)];
    const res = applyMessageEvent(rows, { boxes: ['outbox'], message: msg(9) }, 'inbox', 1, 25, 1);
    expect(res.rows).toBe(rows);
    expect(res.added).toBe(false);
  });

  it('builds the admin search query and leaves out empty filters', () => {
    const q = new URLSearchParams(adminSearchQuery({ src: '', dst: ' 3100 ', format: 'tms', text: '' }, 2, 100));
    expect([...q.keys()]).toEqual(['dst', 'format', 'page', 'limit']);
    expect(q.get('dst')).toBe('3100');
    const tz = process.env.TZ;
    process.env.TZ = 'America/Chicago';
    try {
      expect(dayStart('2026-10-22', 1)).toBe('2026-10-23T05:00:00.000Z');
      expect(dayStart('2026-11-01')).toBe('2026-11-01T05:00:00.000Z');
      expect(dayStart('2026-11-01', 1)).toBe('2026-11-02T06:00:00.000Z');
    } finally {
      process.env.TZ = tz;
    }
    expect(dayStart('bad')).toBeUndefined();
  });

  it('names SAPs and networks and dumps raw bytes', () => {
    expect(sapLabel(4)).toBe('IP (4)');
    expect(sapLabel(10)).toBe('Short data (10)');
    expect(sapLabel(7)).toBe('SAP 7');
    expect(sapLabel(null)).toBe('None');
    expect(transportLabel('openbridge')).toBe('OpenBridge');
    expect(hexDump('0102')).toBe('0000  01 02');
    expect(hexDump('00'.repeat(17)).split('\n')[1]).toBe('0010  00');
  });

  it('does not put a new message on a later page', () => {
    const rows = [msg(1)];
    const res = applyMessageEvent(rows, { boxes: ['inbox'], message: msg(9) }, 'inbox', 2, 25, 5);
    expect(res.rows).toBe(rows);
    expect(res.added).toBe(true);
  });

  it('takes an unknown message that is not newer as a change on another page', () => {
    const rows = [msg(100), msg(99)];
    const first = applyMessageEvent(rows, { boxes: ['inbox'], message: msg(60, { response: 'ack' }) }, 'inbox', 1, 25, 100);
    expect(first.rows).toBe(rows);
    expect(first.added).toBe(false);
    const later = applyMessageEvent(rows, { boxes: ['inbox'], message: msg(60) }, 'inbox', 2, 25, 100);
    expect(later.added).toBe(false);
  });
});
