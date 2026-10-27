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
import { describe, it, expect, vi, type Mock } from 'vitest';
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

import API from '@/services/API';
import UserDetailsPage from '@/views/users/UserDetailsPage.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock };

describe('UserDetailsPage', () => {
  it('shows when the user registered', async () => {
    mockAPI.get.mockImplementation((url: string) => {
      if (url.startsWith('/lastheard')) {
        return Promise.resolve({ data: { calls: [], total: 0 } });
      }
      return Promise.resolve({
        data: {
          id: 3000001, callsign: 'N0DEMO', admin: false, approved: true, repeaters: [],
          created_at: new Date(Date.now() - 3 * 24 * 60 * 60_000).toISOString(),
        },
      });
    });
    const pinia = createPinia();
    setActivePinia(pinia);
    Object.assign(useUserStore(), { loggedIn: true, id: 3000001, admin: false });
    const wrapper = mount(UserDetailsPage, {
      global: {
        plugins: [pinia],
        mocks: { $route: { params: { id: '3000001' } }, $toast: { add: vi.fn() } },
        stubs: { RouterLink: true },
      },
    });
    await flushPromises();
    expect(wrapper.text()).toMatch(/Registered\s*3 days ago/);
    wrapper.unmount();
  });
});
