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

import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}));

import API from '@/services/API';
import OnAirCard from '@/components/OnAirCard.vue';
import { callLength, resetText, type OnAirCall } from '@/lib/sysop';

const mockAPI = API as unknown as { get: Mock; post: Mock };

const call = (id: number, slot: number): OnAirCall => ({
  call_id: id, slot, direction: 'from', source_id: 3191868, callsign: 'KI5VMF', destination_id: 3100, group_call: true,
  start_time: new Date(Date.now() - 42_000).toISOString(),
});

describe('sysop helpers', () => {
  it('shows the length of a call as m:ss', () => {
    expect(callLength('2026-11-01T12:00:00Z', Date.parse('2026-11-01T12:00:42Z'))).toBe('0:42');
    expect(callLength('2026-11-01T12:00:00Z', Date.parse('2026-11-01T12:10:05Z'))).toBe('10:05');
  });

  it('says what a reset ends', () => {
    expect(resetText('hotspot', [])).toBe('The hotspot logs in again by itself.');
    expect(resetText('hotspot', [call(1, 2)])).toBe('The call on TS2 ends. The hotspot logs in again by itself.');
    expect(resetText('repeater', [call(1, 2), call(2, 1)])).toBe('The calls on TS1 and TS2 end. The repeater logs in again by itself.');
  });
});

describe('OnAirCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows nothing without a call', async () => {
    mockAPI.get.mockResolvedValue({ data: { calls: [] } });
    const w = mount(OnAirCard, { props: { repeaterId: 319186801 } });
    await flushPromises();
    expect(w.find('[data-test="on-air"]').exists()).toBe(false);
    w.unmount();
  });

  it('lists a call and drops it', async () => {
    mockAPI.get.mockResolvedValue({ data: { calls: [call(7, 2)] } });
    mockAPI.post.mockResolvedValue({ data: {} });
    const w = mount(OnAirCard, { props: { repeaterId: 319186801 } });
    await flushPromises();
    const row = w.find('[data-test="on-air-7"]');
    expect(row.text()).toContain('TS2');
    expect(row.text()).toContain('KI5VMF');
    expect(row.text()).toContain('TG 3100');
    expect(row.text()).toMatch(/0:4\d/);
    expect(w.emitted('calls')?.[0]).toEqual([[call(7, 2)].map((c) => ({ ...c, start_time: expect.any(String) }))]);
    await w.find('[data-test="drop-7"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/319186801/drop-call', { call_id: 7 });
    expect(w.find('[data-test="on-air"]').exists()).toBe(false);
    w.unmount();
  });

  it('shows why a drop failed', async () => {
    mockAPI.get.mockResolvedValue({ data: { calls: [call(7, 1)] } });
    mockAPI.post.mockRejectedValue({ response: { data: { error: 'That call is not on the air on this repeater' } } });
    const w = mount(OnAirCard, { props: { repeaterId: 1 } });
    await flushPromises();
    await w.find('[data-test="drop-7"]').trigger('click');
    await flushPromises();
    expect(w.find('[data-test="on-air-message"]').text()).toBe('That call is not on the air on this repeater');
    w.unmount();
  });
});
