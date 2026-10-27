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
import { createPinia, setActivePinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn() },
}));
vi.mock('@/services/ws', () => ({
  default: { connect: vi.fn(() => ({ close: vi.fn() })) },
}));
vi.mock('@/services/net', () => ({
  getNets: vi.fn(),
  startNet: vi.fn(),
  stopNet: vi.fn(),
  getScheduledNets: vi.fn(),
  deleteScheduledNet: vi.fn(),
}));

import API from '@/services/API';
import * as net from '@/services/net';
import TalkgroupDetailsPage from '@/views/talkgroups/TalkgroupDetailsPage.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock };
const mockNet = net as unknown as Record<string, Mock>;

const talkgroup = {
  id: 3100, name: 'USA', description: '', created_at: '2026-10-01T00:00:00Z',
  admins: [{ id: 3100003, callsign: 'N0OWN' }], ncos: [{ id: 3100002, callsign: 'N0NCO' }],
};
const activeNet = {
  id: 7, talkgroup_id: 3100, active: true, description: '', check_in_count: 0,
  started_by_user: { id: 3100002, callsign: 'N0NCO' }, start_time: '2026-10-26T00:00:00Z',
};
const scheduled = { id: 9, name: 'Weekly', day_of_week: 3, time_of_day: '19:00', enabled: true };

async function page(user: { loggedIn: boolean; id?: number; admin?: boolean }, active: boolean) {
  const pinia = createPinia();
  setActivePinia(pinia);
  Object.assign(useUserStore(), { id: 0, admin: false, ...user });
  mockAPI.get.mockImplementation((url: string) =>
    Promise.resolve({ data: url.startsWith('/lastheard') ? { calls: [], total: 0 } : talkgroup }));
  mockNet.getNets!.mockResolvedValue({ data: { nets: active ? [activeNet] : [], total: active ? 1 : 0 } });
  mockNet.getScheduledNets!.mockResolvedValue({ data: { scheduled_nets: [scheduled], total: 1 } });
  const wrapper = mount(TalkgroupDetailsPage, {
    global: {
      plugins: [pinia],
      mocks: { $route: { params: { id: '3100' } }, $toast: { add: vi.fn() } },
      stubs: { RouterLink: { template: '<a><slot /></a>' } },
    },
  });
  await flushPromises();
  const buttons = wrapper.findAll('button').map((b) => b.text());
  wrapper.unmount();
  return buttons;
}

describe('TalkgroupDetailsPage net controls', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  for (const [who, user] of [
    ['a visitor', { loggedIn: false }],
    ['a user who is not an NCO or admin of the talkgroup', { loggedIn: true, id: 3100001 }],
  ] as const) {
    it(`hides the net controls from ${who}`, async () => {
      const idle = await page(user, false);
      expect(idle).not.toContain('Start Net');
      expect(idle).not.toContain('Schedule Net');
      expect(idle).not.toContain('Edit');
      expect(idle).not.toContain('Delete');
      expect(await page(user, true)).not.toContain('Stop Net');
    });
  }

  for (const [who, user] of [
    ['an NCO', { loggedIn: true, id: 3100002 }],
    ['an admin of the talkgroup', { loggedIn: true, id: 3100003 }],
    ['an admin', { loggedIn: true, id: 3100009, admin: true }],
  ] as const) {
    it(`shows the net controls to ${who}`, async () => {
      const idle = await page(user, false);
      expect(idle).toContain('Start Net');
      expect(idle).toContain('Schedule Net');
      expect(idle).toContain('Edit');
      expect(idle).toContain('Delete');
      expect(await page(user, true)).toContain('Stop Net');
    });
  }
});
