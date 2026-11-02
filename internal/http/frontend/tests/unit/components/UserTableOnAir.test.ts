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
  default: {
    get: vi.fn(),
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
import UserTable from '@/components/UserTable.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; delete: Mock };

const row = (id: number, callsign: string) => ({
  id,
  callsign,
  username: callsign.toLowerCase(),
  approved: true,
  suspended: false,
  admin: false,
  repeaters: [],
  created_at: '2026-10-01T00:00:00Z',
});

describe('UserTable on-air code', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
    useUserStore().id = 1;
  });

  it('offers to turn off the on-air code only of users who have it on', async () => {
    mockAPI.get.mockResolvedValue({ data: { total: 2, users: [row(1000001, 'N0CALL'), row(1000002, 'N1CALL')], on_air_codes: [1000002] } });
    mockAPI.delete.mockResolvedValue({ data: {} });
    const add = vi.fn();
    const require = vi.fn((o: { accept: () => void }) => o.accept());
    const wrapper = mount(UserTable, {
      attachTo: document.body,
      global: { mocks: { $toast: { add }, $confirm: { require } } },
    });
    await flushPromises();
    const tr = (callsign: string) => wrapper.findAll('tr').find((r) => r.text().includes(callsign))!;
    const button = (callsign: string) => tr(callsign).findAll('button').find((b) => b.text() === 'Turn off on-air code');
    expect(button('N0CALL')).toBeUndefined();
    await button('N1CALL')!.trigger('click');
    await flushPromises();
    expect(require).toHaveBeenCalledWith(expect.objectContaining({ header: 'Turn off on-air code' }));
    expect(mockAPI.delete).toHaveBeenCalledWith('/users/1000002/totp');
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
    expect(button('N1CALL')).toBeUndefined();
  });

  it('shows no on-air button when the API sends no list', async () => {
    mockAPI.get.mockResolvedValue({ data: { total: 1, users: [row(1000002, 'N1CALL')] } });
    const wrapper = mount(UserTable, {
      attachTo: document.body,
      global: { mocks: { $toast: { add: vi.fn() }, $confirm: { require: vi.fn() } } },
    });
    await flushPromises();
    expect(wrapper.text()).not.toContain('Turn off on-air code');
  });
});
