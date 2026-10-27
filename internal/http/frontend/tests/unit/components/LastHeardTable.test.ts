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

import API from '@/services/API';
import ws from '@/services/ws';
import LastHeardTable from '@/components/LastHeardTable.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock };
const mockWS = ws as unknown as { connect: Mock };

async function socketPath(loggedIn: boolean) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useUserStore().loggedIn = loggedIn;
  mockAPI.get.mockResolvedValue({ data: { calls: [], total: 0 } });
  const wrapper = mount(LastHeardTable, { global: { plugins: [pinia], stubs: { RouterLink: true } } });
  await flushPromises();
  wrapper.unmount();
  return new URL(mockWS.connect.mock.calls[0]![0] as string).pathname;
}

describe('LastHeardTable live updates', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('uses the public websocket for a visitor', async () => {
    expect(await socketPath(false)).toBe('/ws/lastheard');
  });

  it('uses the calls websocket for a logged-in user', async () => {
    expect(await socketPath(true)).toBe('/ws/calls');
  });

  it('moves to the calls websocket when the visitor logs in', async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const user = useUserStore();
    mockAPI.get.mockResolvedValue({ data: { calls: [], total: 0 } });
    const wrapper = mount(LastHeardTable, { global: { plugins: [pinia], stubs: { RouterLink: true } } });
    await flushPromises();
    user.loggedIn = true;
    await flushPromises();
    const paths = mockWS.connect.mock.calls.map((c) => new URL(c[0] as string).pathname);
    expect(paths).toEqual(['/ws/lastheard', '/ws/calls']);
    expect(mockWS.connect.mock.results[0]!.value.close).toHaveBeenCalled();
    wrapper.unmount();
  });
});
