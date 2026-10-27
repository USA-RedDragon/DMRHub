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
  default: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
}));
vi.mock('@/services/ws', () => ({
  default: { connect: vi.fn(() => ({ close: vi.fn() })) },
}));

import API from '@/services/API';
import RepeaterTable from '@/components/RepeaterTable.vue';

const mockAPI = API as unknown as { get: Mock };

function repeater(slots: number) {
  const t = new Date().toISOString();
  return {
    id: 310000101, type: 'mmdvm', slots, connected_time: t, created_at: t, last_ping_time: t,
    ts1_static_talkgroups: [], ts2_static_talkgroups: [],
    ts1_dynamic_talkgroup: { id: 0, name: '' }, ts2_dynamic_talkgroup: { id: 0, name: '' },
    hotspot: true, simplex_repeater: false, trusted: false,
  };
}

async function editing(slots: number) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/talkgroups')) {
      return Promise.resolve({ data: { talkgroups: [{ id: 3100, name: 'USA' }] } });
    }
    return Promise.resolve({ data: { repeaters: [repeater(slots)], total: 1 } });
  });
  const wrapper = mount(RepeaterTable, {
    global: { mocks: { $toast: { add: vi.fn() }, $confirm: { require: vi.fn() } }, stubs: { RouterLink: true } },
  });
  await flushPromises();
  const edit = wrapper.findAll('button').find((b) => b.text() === 'Edit');
  await edit!.trigger('click');
  await flushPromises();
  return {
    ts1: wrapper.find('select#ts1_static_talkgroups:not([disabled])').exists()
      && wrapper.find('select#ts1_dynamic_talkgroup:not([disabled])').exists(),
    ts2: wrapper.find('select#ts2_static_talkgroups:not([disabled])').exists()
      && wrapper.find('select#ts2_dynamic_talkgroup:not([disabled])').exists(),
  };
}

describe('RepeaterTable timeslots', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  for (const [slots, ts1, ts2] of [[1, true, false], [2, false, true], [3, true, true], [4, false, true], [0, true, true]] as const) {
    it(`edits TS1 ${ts1} and TS2 ${ts2} for an RPTC Slots of ${slots}`, async () => {
      expect(await editing(slots)).toEqual({ ts1, ts2 });
    });
  }
});
