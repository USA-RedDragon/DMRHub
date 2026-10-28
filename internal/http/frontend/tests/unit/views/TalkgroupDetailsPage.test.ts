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
import { createPinia, setActivePinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn() },
}));
vi.mock('@/services/ws', () => ({
  default: { connect: vi.fn(() => ({ close: vi.fn() })) },
}));
vi.mock('@/services/userdb', () => ({
  getUserDB: vi.fn(() => Promise.resolve(null)),
}));
vi.mock('@/services/net', () => ({
  getNets: vi.fn(),
  startNet: vi.fn(),
  stopNet: vi.fn(),
  getScheduledNets: vi.fn(),
  deleteScheduledNet: vi.fn(),
}));

import API from '@/services/API';
import ws from '@/services/ws';
import * as net from '@/services/net';
import TalkgroupDetailsPage from '@/views/talkgroups/TalkgroupDetailsPage.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock };
const mockNet = net as unknown as Record<string, Mock>;
const mockWS = ws as unknown as { connect: Mock };

const talkgroup = {
  id: 3100, name: 'USA', description: '', created_at: '2026-10-01T00:00:00Z',
  admins: [{ id: 3100003, callsign: 'N0OWN' }], ncos: [{ id: 3100002, callsign: 'N0NCO' }],
};
const activeNet = {
  id: 7, talkgroup_id: 3100, active: true, description: '', check_in_count: 0,
  started_by_user: { id: 3100002, callsign: 'N0NCO' }, start_time: '2026-10-26T00:00:00Z',
};
const access = { mode: 'approved', repeaters: [{ id: 311860, callsign: 'KI5VMF' }] };
const scheduled = { id: 9, name: 'Weekly', day_of_week: 3, time_of_day: '19:00', timezone: 'America/Chicago', enabled: true };

async function page(user: { loggedIn: boolean; id?: number; admin?: boolean }, active: boolean) {
  const pinia = createPinia();
  setActivePinia(pinia);
  Object.assign(useUserStore(), { id: 0, admin: false, ...user });
  mockAPI.get.mockImplementation((url: string) =>
    Promise.resolve({ data: url.startsWith('/lastheard') ? { calls: [], total: 0 } : url.endsWith('/access') ? access : talkgroup }));
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
  lastText = wrapper.text();
  wrapper.unmount();
  return buttons;
}

let lastText = '';

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

describe('TalkgroupDetailsPage details', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows when the talkgroup was made and its admins and NCOs', async () => {
    await page({ loggedIn: false }, false);
    expect(lastText).toMatch(/Created\s*\d+ \w+ ago/);
    expect(lastText).toContain('N0OWN');
    expect(lastText).toContain('N0NCO');
  });
});

describe('TalkgroupDetailsPage access', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  for (const [who, user] of [
    ['a visitor', { loggedIn: false }],
    ['a user who is not an admin of the talkgroup', { loggedIn: true, id: 3100001 }],
    ['an NCO', { loggedIn: true, id: 3100002 }],
  ] as const) {
    it(`shows no access card to ${who}`, async () => {
      await page(user, false);
      expect(lastText).not.toContain('Who can link');
      expect(mockAPI.get).not.toHaveBeenCalledWith('/talkgroups/3100/access');
    });
  }

  for (const [who, user] of [
    ['an admin of the talkgroup', { loggedIn: true, id: 3100003 }],
    ['an admin', { loggedIn: true, id: 3100009, admin: true }],
  ] as const) {
    it(`shows the access card to ${who}`, async () => {
      await page(user, false);
      expect(lastText).toContain('Who can link');
      expect(lastText).toContain('KI5VMF');
    });
  }
});

describe('TalkgroupDetailsPage scheduled nets', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the time of a scheduled net in its own time zone', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-10-27T12:00:00Z'));
    await page({ loggedIn: false }, false);
    expect(lastText).toContain('Wed at 14:00 America/Chicago');
  });
});

describe('TalkgroupDetailsPage live calls', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const callPaths = () => mockWS.connect.mock.calls
    .map((c) => new URL(c[0] as string).pathname)
    .filter((p) => !p.startsWith('/ws/nets'));

  it('uses the public websocket for a visitor', async () => {
    await page({ loggedIn: false }, false);
    expect(callPaths()).toEqual(['/ws/lastheard']);
  });

  it('uses the calls websocket for a logged-in user', async () => {
    await page({ loggedIn: true, id: 3000001 }, false);
    expect(callPaths()).toEqual(['/ws/calls']);
  });

  it('moves to the calls websocket when the visitor logs in', async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const user = useUserStore();
    Object.assign(user, { id: 0, admin: false, loggedIn: false });
    mockAPI.get.mockImplementation((url: string) =>
      Promise.resolve({ data: url.startsWith('/lastheard') ? { calls: [], total: 0 } : talkgroup }));
    mockNet.getNets!.mockResolvedValue({ data: { nets: [], total: 0 } });
    mockNet.getScheduledNets!.mockResolvedValue({ data: { scheduled_nets: [], total: 0 } });
    const wrapper = mount(TalkgroupDetailsPage, {
      global: {
        plugins: [pinia],
        mocks: { $route: { params: { id: '3100' } }, $toast: { add: vi.fn() } },
        stubs: { RouterLink: { template: '<a><slot /></a>' } },
      },
    });
    await flushPromises();
    user.loggedIn = true;
    await flushPromises();
    expect(callPaths()).toEqual(['/ws/lastheard', '/ws/calls']);
    const first = mockWS.connect.mock.calls.findIndex((c) => new URL(c[0] as string).pathname === '/ws/lastheard');
    expect(mockWS.connect.mock.results[first]!.value.close).toHaveBeenCalled();
    wrapper.unmount();
  });
});
