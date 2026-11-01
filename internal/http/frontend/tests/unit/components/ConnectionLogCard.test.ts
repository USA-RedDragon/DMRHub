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
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn() },
}));

import API from '@/services/API';
import ConnectionLogCard from '@/components/ConnectionLogCard.vue';
import { logLine, type RepeaterEvent } from '@/lib/connectionLog';

const mockAPI = API as unknown as { get: Mock };

const ev = (id: number, kind: string, extra: Partial<RepeaterEvent> = {}): RepeaterEvent => ({
  id, kind, first_at: '2026-11-01T13:12:00Z', last_at: '2026-11-01T14:41:00Z', count: 1, address: '', detail: '', ...extra,
});

describe('logLine', () => {
  it('says what a wrong password is and what to change, with the folded count', () => {
    const l = logLine(ev(1, 'wrong_password', { address: '203.0.113.7', count: 14 }), 319186801, 'hotspot');
    expect(l.title).toBe('Login refused: wrong password.');
    expect(l.note).toMatch(/^From 203\.0\.113\.7, 14 times since \d\d:\d\d$/);
    expect(l.fix).toContain('Rotate the password');
    expect(l.problem).toBe(true);
    expect(l.tone).toBe('bad');
  });

  it('names the ID that two devices share when one pushes the other off', () => {
    const l = logLine(ev(2, 'pushed_off', { address: '198.51.100.24' }), 319186801, 'hotspot');
    expect(l.fix).toBe('Two devices use ID 319186801. Give each one its own ID: your DMR ID and two more digits.');
  });

  it('shows a login with its address and software as good news', () => {
    const l = logLine(ev(3, 'connected', { address: '203.0.113.7', detail: 'Pi-Star 4.2.1, MMDVM_HS_Hat' }), 1, 'hotspot');
    expect(l.title).toBe('Connected.');
    expect(l.note).toBe('From 203.0.113.7, Pi-Star 4.2.1, MMDVM_HS_Hat');
    expect(l.problem).toBe(false);
    expect(l.tone).toBe('good');
  });

  it('words the timeout, the unknown talkgroups and the logouts', () => {
    expect(logLine(ev(4, 'timeout', { detail: '60' }), 1, 'repeater').title).toBe('Connection lost: no ping for 60 seconds.');
    expect(logLine(ev(4, 'timeout'), 1, 'repeater').fix).toBe('Check the internet connection of the repeater. It logs in again by itself.');
    expect(logLine(ev(5, 'unknown_talkgroup', { detail: '31665' }), 1, 'hotspot').title)
      .toBe('Talkgroup 31665 in the hotspot options does not exist.');
    expect(logLine(ev(5, 'unknown_talkgroup', { detail: '1,2' }), 1, 'hotspot').title)
      .toBe('Talkgroups 1, 2 in the hotspot options do not exist.');
    expect(logLine(ev(6, 'password_rotated'), 1, 'hotspot').note).toBe('The password was rotated');
    expect(logLine(ev(7, 'reset', { detail: 'KP4DJT' }), 1, 'hotspot').note).toBe('KP4DJT reset the connection');
    expect(logLine(ev(8, 'closed'), 1, 'hotspot').problem).toBe(false);
  });
});

describe('ConnectionLogCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lists the entries and hides the rest with Problems only', async () => {
    mockAPI.get.mockResolvedValue({ data: { events: [ev(1, 'timeout', { detail: '60' }), ev(2, 'connected', { address: '203.0.113.7' })] } });
    const w = mount(ConnectionLogCard, { props: { repeaterId: 319186801, device: 'hotspot' } });
    await flushPromises();
    expect(mockAPI.get).toHaveBeenCalledWith('/repeaters/319186801/events');
    expect(w.find('[data-test="log-1"]').text()).toContain('Connection lost: no ping for 60 seconds.');
    expect(w.find('[data-test="log-2"]').text()).toContain('Connected.');

    await w.find('[data-test="problems-only"]').trigger('click');
    await flushPromises();
    expect(w.find('[data-test="log-1"]').exists()).toBe(true);
    expect(w.find('[data-test="log-2"]').exists()).toBe(false);
    w.unmount();
  });

  it('says when the last day had nothing', async () => {
    mockAPI.get.mockResolvedValue({ data: { events: [] } });
    const w = mount(ConnectionLogCard, { props: { repeaterId: 1, device: 'hotspot' } });
    await flushPromises();
    expect(w.find('[data-test="log-empty"]').text()).toBe('Nothing in the last 24 hours.');
    w.unmount();
  });
});
