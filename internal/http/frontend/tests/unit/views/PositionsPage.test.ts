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
import { createPinia } from 'pinia';
import type { Position } from '@/services/positions';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    put: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

vi.mock('@/components/map/PositionsMap.vue', () => ({
  default: {
    name: 'PositionsMap',
    props: ['tracks', 'selected'],
    template: '<div data-test="map" />',
  },
}));

const socket = vi.hoisted(() => ({
  handler: null as null | ((event: { data: string }) => void),
  url: '',
  closed: false,
}));

vi.mock('@/services/ws', () => ({
  default: {
    connect: (url: string, handler: (event: { data: string }) => void) => {
      socket.url = url;
      socket.handler = handler;
      return { close: () => { socket.closed = true; } };
    },
  },
}));

import API from '@/services/API';
import PositionsPage from '@/views/positions/PositionsPage.vue';

const mockAPI = API as unknown as { get: Mock; put: Mock };

const pos = (radio: number, minutesAgo: number, over: Partial<Position> = {}): Position => ({
  radio_id: radio,
  shared: false,
  latitude: 30.2672,
  longitude: -97.7431,
  format: 'lrrp',
  accuracy_m: 10,
  speed_mps: null,
  heading_deg: null,
  altitude_m: null,
  fix_time: null,
  received_at: new Date(Date.now() - minutesAgo * 60000).toISOString(),
  repeater_id: 319186801,
  slot: 2,
  service_id: 5057,
  ...over,
});

const shared = pos(3140598, 60, { shared: true, callsign: 'KD5SMP' });

function serve(share = true) {
  mockAPI.get.mockImplementation((url: string, opts?: { params?: { hours?: number } }) => {
    if (url === '/positions/radios') {
      return Promise.resolve({
        data: {
          radios: [
            { radio_id: 3191868, tracking: true, interval_minutes: 10, retention_hours: 24, aprs: true, aprs_ssid: '9', aprs_symbol: '', aprs_comment: '' },
            { radio_id: 3191899, tracking: false, interval_minutes: 10, retention_hours: 24, aprs: false, aprs_ssid: '', aprs_symbol: '', aprs_comment: '' },
          ],
          intervals: [5, 10, 15, 30, 60],
          retention_hours: [24, 168, 720],
          aprs: true,
          callsign: 'KI5VMF',
        },
      });
    }
    if (url === '/users/me/settings') {
      return Promise.resolve({ data: { sms_storage: false, share_positions: share } });
    }
    if (url === '/positions') {
      return Promise.resolve({ data: { positions: share ? [pos(3191868, 4), shared] : [pos(3191868, 4)] } });
    }
    if (url === '/positions/3191868') {
      const hours = opts?.params?.hours ?? 24;
      const track = hours === 24 ? [pos(3191868, 20), pos(3191868, 4)] : [pos(3191868, 3000), pos(3191868, 20), pos(3191868, 4)];
      return Promise.resolve({ data: { latest: track[track.length - 1], track } });
    }
    if (url === '/positions/3140598') {
      return Promise.resolve({ data: { latest: shared, track: [shared] } });
    }
    return Promise.reject({ response: { status: 404 } });
  });
}

async function mountPage() {
  const wrapper = mount(PositionsPage, {
    attachTo: document.body,
    global: {
      plugins: [createPinia()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return wrapper;
}

describe('PositionsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    socket.handler = null;
    socket.url = '';
    socket.closed = false;
  });

  it('lists the users radios, tracked or not, then the shared ones', async () => {
    serve();
    const wrapper = await mountPage();
    const mine = wrapper.find('[data-test="radio-3191868"]').text();
    expect(mine).toContain('4 min ago');
    expect(mine).toContain('30.2672, −97.7431 · LRRP');
    expect(mine).toContain('Tracking every 10 min · APRS-IS as KI5VMF-9');
    expect(wrapper.find('[data-test="radio-3191868"] a').attributes('href')).toBe('/positions/3191868');
    expect(wrapper.find('[data-test="radio-3191899"]').text()).toContain('Tracking is off');
    const theirs = wrapper.find('[data-test="radio-3140598"]');
    expect(theirs.text()).toContain('3140598 · KD5SMP');
    expect(theirs.text()).toContain('Shared by KD5SMP');
    expect(theirs.find('a').exists()).toBe(false);
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    const tracks = map.props('tracks') as { radio: number; points: Position[] }[];
    expect(tracks.map((t) => [t.radio, t.points.length])).toEqual([[3191868, 2], [3140598, 1]]);
    expect(socket.url).toMatch(/\/messages$/);
  });

  it('hides the shared radios when asked', async () => {
    serve();
    const wrapper = await mountPage();
    await wrapper.find('[data-test="show-shared"]').setValue(false);
    expect(wrapper.find('[data-test="radio-3140598"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="radio-3191868"]').exists()).toBe(true);
  });

  it('loads the tracks of the window it shows', async () => {
    serve();
    const wrapper = await mountPage();
    await wrapper.find('[data-test="window-168"]').trigger('click');
    await flushPromises();
    expect(mockAPI.get).toHaveBeenCalledWith('/positions', { params: { hours: 168 } });
    expect(mockAPI.get).toHaveBeenCalledWith('/positions/3191868', { params: { hours: 168 } });
    const tracks = wrapper.findComponent({ name: 'PositionsMap' }).props('tracks') as { radio: number; points: Position[] }[];
    expect(tracks[0]!.points).toHaveLength(3);
    expect(wrapper.find('[data-test="window-168"]').attributes('aria-pressed')).toBe('true');
  });

  it('adds the positions the socket pushes and ignores messages', async () => {
    serve();
    const wrapper = await mountPage();
    const fresh = pos(3191868, 0, { latitude: 30.3 });
    socket.handler!({ data: JSON.stringify({ boxes: ['inbox'], message: { id: 1 } }) });
    socket.handler!({ data: JSON.stringify({ kind: 'position', position: fresh }) });
    socket.handler!({ data: JSON.stringify({ kind: 'position', position: fresh }) });
    await flushPromises();
    const tracks = wrapper.findComponent({ name: 'PositionsMap' }).props('tracks') as { radio: number; points: Position[] }[];
    expect(tracks[0]!.points).toHaveLength(3);
    expect(wrapper.find('[data-test="radio-3191868"]').text()).toContain('30.3000');
    expect(wrapper.find('[data-test="radio-3191868"]').text()).toContain('just now');
  });

  it('turns sharing on and off', async () => {
    serve(false);
    mockAPI.put.mockResolvedValue({ data: { sms_storage: false, share_positions: true } });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="radio-3140598"]').exists()).toBe(false);
    serve(true);
    wrapper.findComponent({ name: 'Switch' }).vm.$emit('update:modelValue', true);
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/share-positions', { enabled: true });
    expect(wrapper.find('[data-test="radio-3140598"]').exists()).toBe(true);
  });

  it('asks a visitor to log in', async () => {
    mockAPI.get.mockRejectedValue({ response: { status: 401 } });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="positions-anon"]').exists()).toBe(true);
    expect(socket.handler).toBeNull();
  });

  it('closes the socket when it leaves', async () => {
    serve();
    const wrapper = await mountPage();
    wrapper.unmount();
    expect(socket.closed).toBe(true);
  });
});
