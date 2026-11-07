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
    props: ['tracks', 'selected', 'repeaters', 'selectedRepeater', 'blipRadios', 'blipRepeaters', 'lines'],
    template: '<div data-test="map" />',
  },
}));

type Handler = (event: { data: string }) => void;

const sockets = vi.hoisted(() => new Map<string, { handler: (event: { data: string }) => void; closed: boolean }>());

vi.mock('@/services/ws', () => ({
  default: {
    connect: (url: string, handler: (event: { data: string }) => void) => {
      const s = { handler, closed: false };
      sockets.set(url.slice(url.lastIndexOf('/')), s);
      return { close: () => { s.closed = true; } };
    },
  },
}));

const send = (path: string, frame: unknown) => {
  const handler: Handler = sockets.get(path)!.handler;
  handler({ data: JSON.stringify(frame) });
};

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
  repeater_id: 300206801,
  slot: 2,
  service_id: 5057,
  ...over,
});

const shared = pos(3002198, 60, { shared: true, callsign: 'N8CALL' });

const repeater = {
  id: 310999901,
  callsign: 'N0CALL',
  latitude: 30.2669,
  longitude: -97.7729,
  height: 12,
  location: 'Zilker Park',
  description: 'Test repeater',
  connected_time: new Date(Date.now() - 2 * 3600000).toISOString(),
  last_ping_time: new Date().toISOString(),
};

function serve(share = true, repeaters: (typeof repeater)[] = []) {
  mockAPI.get.mockImplementation((url: string, opts?: { params?: { hours?: number } }) => {
    if (url === '/positions/repeaters') {
      return Promise.resolve({ data: { repeaters } });
    }
    if (url === '/positions/radios') {
      return Promise.resolve({
        data: {
          radios: [
            { radio_id: 3002068, tracking: true, interval_minutes: 10, retention_hours: 24, aprs: true, aprs_ssid: '9', aprs_symbol: '', aprs_comment: '' },
            { radio_id: 3002099, tracking: false, interval_minutes: 10, retention_hours: 24, aprs: false, aprs_ssid: '', aprs_symbol: '', aprs_comment: '' },
          ],
          intervals: [5, 10, 15, 30, 60],
          retention_hours: [24, 168, 720],
          aprs: true,
          callsign: 'N6CALL',
        },
      });
    }
    if (url === '/users/me/settings') {
      return Promise.resolve({ data: { sms_storage: false, share_positions: share } });
    }
    if (url === '/positions') {
      return Promise.resolve({ data: { positions: share ? [pos(3002068, 4), shared] : [pos(3002068, 4)] } });
    }
    if (url === '/positions/3002068') {
      const hours = opts?.params?.hours ?? 24;
      const track = hours === 24 ? [pos(3002068, 20), pos(3002068, 4)] : [pos(3002068, 3000), pos(3002068, 20), pos(3002068, 4)];
      return Promise.resolve({ data: { latest: track[track.length - 1], track } });
    }
    if (url === '/positions/3002198') {
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
    sockets.clear();
    vi.useRealTimers();
  });

  it('lists the users radios, tracked or not, then the shared ones', async () => {
    serve();
    const wrapper = await mountPage();
    const mine = wrapper.find('[data-test="radio-3002068"]').text();
    expect(mine).toContain('4 min ago');
    expect(mine).toContain('30.2672, −97.7431 · LRRP');
    expect(mine).toContain('Tracking every 10 min · APRS-IS as N6CALL-9');
    expect(wrapper.find('[data-test="radio-3002068"] a').attributes('href')).toBe('/positions/3002068');
    expect(wrapper.find('[data-test="radio-3002099"]').text()).toContain('Tracking is off');
    const theirs = wrapper.find('[data-test="radio-3002198"]');
    expect(theirs.text()).toContain('3002198 · N8CALL');
    expect(theirs.text()).toContain('Shared by N8CALL');
    expect(theirs.find('a').exists()).toBe(false);
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    const tracks = map.props('tracks') as { radio: number; points: Position[] }[];
    expect(tracks.map((t) => [t.radio, t.points.length])).toEqual([[3002068, 2], [3002198, 1]]);
    expect([...sockets.keys()].sort()).toEqual(['/calls', '/lastheard', '/messages']);
  });

  it('hides the shared radios when asked', async () => {
    serve();
    const wrapper = await mountPage();
    await wrapper.find('[data-test="show-shared"]').setValue(false);
    expect(wrapper.find('[data-test="radio-3002198"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="radio-3002068"]').exists()).toBe(true);
  });

  it('loads the tracks of the window it shows', async () => {
    serve();
    const wrapper = await mountPage();
    await wrapper.find('[data-test="window-168"]').trigger('click');
    await flushPromises();
    expect(mockAPI.get).toHaveBeenCalledWith('/positions', { params: { hours: 168 } });
    expect(mockAPI.get).toHaveBeenCalledWith('/positions/3002068', { params: { hours: 168 } });
    const tracks = wrapper.findComponent({ name: 'PositionsMap' }).props('tracks') as { radio: number; points: Position[] }[];
    expect(tracks[0]!.points).toHaveLength(3);
    expect(wrapper.find('[data-test="window-168"]').attributes('aria-pressed')).toBe('true');
  });

  it('adds the positions the socket pushes and ignores messages', async () => {
    serve();
    const wrapper = await mountPage();
    const fresh = pos(3002068, 0, { latitude: 30.3 });
    send('/messages', { boxes: ['inbox'], message: { id: 1 } });
    send('/messages', { kind: 'position', position: fresh });
    send('/messages', { kind: 'position', position: fresh });
    await flushPromises();
    const tracks = wrapper.findComponent({ name: 'PositionsMap' }).props('tracks') as { radio: number; points: Position[] }[];
    expect(tracks[0]!.points).toHaveLength(3);
    expect(wrapper.find('[data-test="radio-3002068"]').text()).toContain('30.3000');
    expect(wrapper.find('[data-test="radio-3002068"]').text()).toContain('just now');
  });

  it('turns sharing on and off', async () => {
    serve(false);
    mockAPI.put.mockResolvedValue({ data: { sms_storage: false, share_positions: true } });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="radio-3002198"]').exists()).toBe(false);
    serve(true);
    wrapper.findComponent({ name: 'Switch' }).vm.$emit('update:modelValue', true);
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/share-positions', { enabled: true });
    expect(wrapper.find('[data-test="radio-3002198"]').exists()).toBe(true);
  });

  it('lists the connected repeaters that report a position and pins them on the map', async () => {
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const row = wrapper.find('[data-test="repeater-310999901"]');
    expect(row.text()).toContain('N0CALL · 310999901');
    expect(row.text()).toContain('30.2669, −97.7729');
    expect(row.text()).toContain('Zilker Park · Test repeater');
    expect(row.text()).toContain('2 hours ago');
    expect(row.find('a').attributes('href')).toBe('/repeaters/310999901');
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    const pins = map.props('repeaters') as { id: number; title: string; latitude: number; longitude: number }[];
    expect(pins.map((p) => [p.id, p.title, p.latitude, p.longitude])).toEqual([[310999901, 'N0CALL · 310999901', 30.2669, -97.7729]]);
    await row.trigger('click');
    expect(map.props('selectedRepeater')).toBe(310999901);
    expect(map.props('selected')).toBeNull();
    await wrapper.find('[data-test="radio-3002068"]').trigger('click');
    expect(map.props('selectedRepeater')).toBeNull();
    expect(map.props('selected')).toBe(3002068);
  });

  it('shows no repeater section when no repeater reports a position', async () => {
    serve();
    const wrapper = await mountPage();
    expect(mockAPI.get).toHaveBeenCalledWith('/positions/repeaters');
    expect(wrapper.find('[data-test="repeaters"]').exists()).toBe(false);
    expect(wrapper.findComponent({ name: 'PositionsMap' }).props('repeaters')).toEqual([]);
  });

  it('asks a visitor to log in', async () => {
    mockAPI.get.mockRejectedValue({ response: { status: 401 } });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="positions-anon"]').exists()).toBe(true);
    expect(sockets.size).toBe(0);
  });

  it('closes the socket when it leaves', async () => {
    serve();
    const wrapper = await mountPage();
    wrapper.unmount();
    expect([...sockets.values()].every((s) => s.closed)).toBe(true);
  });

  it('blips a radio and the repeater it came in on while a call is on the air', async () => {
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    const call = { id: 11, active: true, kind: 'voice', source_id: 3002068, repeater_id: 310999901 };
    send('/calls', call);
    send('/lastheard', call);
    await flushPromises();
    expect(map.props('blipRadios')).toEqual({ 3002068: 'loop' });
    expect(map.props('blipRepeaters')).toEqual({ 310999901: 'loop' });
    expect(wrapper.find('[data-test="radio-blip-3002068"]').classes()).toContain('tx-blip--loop');
    expect(wrapper.find('[data-test="repeater-blip-310999901"]').exists()).toBe(true);

    send('/lastheard', { ...call, active: false });
    await flushPromises();
    expect(map.props('blipRadios')).toEqual({});
    expect(map.props('blipRepeaters')).toEqual({});
    expect(wrapper.find('[data-test="radio-blip-3002068"]').exists()).toBe(false);
  });

  it('stops a blip whose call never ends after the timeout', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    send('/calls', { id: 12, active: true, kind: 'voice', source_id: 3002068, repeater_id: 310999901 });
    await flushPromises();
    expect(map.props('blipRadios')).toEqual({ 3002068: 'loop' });
    await vi.advanceTimersByTimeAsync(4999);
    expect(map.props('blipRadios')).toEqual({ 3002068: 'loop' });
    await vi.advanceTimersByTimeAsync(1);
    expect(map.props('blipRadios')).toEqual({});
    expect(map.props('blipRepeaters')).toEqual({});
  });

  it('gives a pushed position one short blip', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    send('/messages', { kind: 'position', position: pos(3002068, 0, { repeater_id: 310999901 }) });
    await flushPromises();
    expect(map.props('blipRadios')).toEqual({ 3002068: 'burst' });
    expect(map.props('blipRepeaters')).toEqual({ 310999901: 'burst' });
    await vi.advanceTimersByTimeAsync(1000);
    expect(map.props('blipRadios')).toEqual({});
  });

  it('blips only the radios and repeaters on the map', async () => {
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    send('/calls', { id: 13, active: true, kind: 'voice', source_id: 3002099, repeater_id: 310999901 });
    send('/lastheard', { id: 14, active: true, kind: 'voice', source_id: 3108888, repeater_id: 310888801 });
    await flushPromises();
    expect(map.props('blipRadios')).toEqual({});
    expect(map.props('blipRepeaters')).toEqual({ 310999901: 'loop' });

    send('/calls', { id: 15, active: true, kind: 'voice', source_id: 3002198, repeater_id: 0 });
    await flushPromises();
    expect(map.props('blipRadios')).toEqual({ 3002198: 'loop' });
    await wrapper.find('[data-test="show-shared"]').setValue(false);
    expect(map.props('blipRadios')).toEqual({});
  });

  it('draws a line between the two radios of a private call while it is on the air', async () => {
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    const call = { id: 21, active: true, kind: 'voice', source_id: 3002068, repeater_id: 310999901, group_call: false, is_to_user: true, destination_id: 3002198 };
    send('/calls', call);
    await flushPromises();
    const lines = map.props('lines') as { key: string; from: { id: number }; to: { id: number }; data: boolean }[];
    expect(lines.map((l) => [l.from.id, l.to.id, l.data])).toEqual([[3002068, 3002198, false]]);
    send('/calls', { ...call, active: false });
    await flushPromises();
    expect(map.props('lines')).toEqual([]);
  });

  it('draws a talkgroup call to the other repeaters that carry the talkgroup', async () => {
    const far = { ...repeater, id: 310999903, longitude: -97.74, talkgroups: [3100] };
    serve(true, [{ ...repeater, talkgroups: [3100] }, far]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    send('/lastheard', { id: 22, active: true, kind: 'voice', source_id: 3002068, repeater_id: 310999901, group_call: true, is_to_talkgroup: true, to_talkgroup: { id: 3100 }, destination_id: 3100 });
    await flushPromises();
    const lines = map.props('lines') as { from: { kind: string; id: number }; to: { kind: string; id: number } }[];
    expect(lines.map((l) => [l.from.kind, l.from.id, l.to.kind, l.to.id])).toEqual([['radio', 3002068, 'repeater', 310999903]]);
    expect(map.props('blipRepeaters')).toEqual({ 310999901: 'loop' });
  });

  it('sends the dot of a data call to the receiver, rings it, then drops the line', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    send('/calls', { id: 23, active: false, kind: 'data', sub_kind: 'sms', source_id: 3002068, repeater_id: 310999901, group_call: false, is_to_user: true, destination_id: 3002198 });
    await flushPromises();
    const lines = () => (map.props('lines') as { data: boolean; to: { id: number } }[]).map((l) => [l.to.id, l.data]);
    expect(lines()).toEqual([[3002198, true]]);
    expect(map.props('blipRadios')).toEqual({ 3002068: 'burst' });
    await vi.advanceTimersByTimeAsync(900);
    expect(map.props('blipRadios')).toEqual({ 3002068: 'burst', 3002198: 'burst' });
    await vi.advanceTimersByTimeAsync(900);
    expect(lines()).toEqual([]);
    await vi.advanceTimersByTimeAsync(100);
    expect(map.props('blipRadios')).toEqual({});
  });

  it('rings only the sender of a data call to a radio that is not on the map', async () => {
    serve(true, [repeater]);
    const wrapper = await mountPage();
    const map = wrapper.findComponent({ name: 'PositionsMap' });
    send('/calls', { id: 24, active: false, kind: 'data', sub_kind: 'lrrp', source_id: 3002068, repeater_id: 310999901, group_call: false, is_to_user: true, destination_id: 5057 });
    await flushPromises();
    expect(map.props('lines')).toEqual([]);
    expect(map.props('blipRadios')).toEqual({ 3002068: 'burst' });
  });
});
