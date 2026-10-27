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
    patch: vi.fn(),
  },
}));

import API from '@/services/API';
import UserTable from '@/components/UserTable.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

const row = (id: number, callsign: string, username: string) => ({
  id,
  callsign,
  username,
  approved: true,
  suspended: false,
  admin: false,
  repeaters: [],
  created_at: '2026-10-01T00:00:00Z',
});

async function saveCallsign(callsign: string) {
  mockAPI.get.mockResolvedValue({ data: { total: 1, users: [row(3191868, 'KI5VMF', 'testuser')] } });
  const add = vi.fn();
  const wrapper = mount(UserTable, {
    attachTo: document.body,
    global: { mocks: { $toast: { add }, $confirm: { require: vi.fn() } } },
  });
  await flushPromises();
  await wrapper.findAll('button').find((b) => b.text() === 'Edit')!.trigger('click');
  await flushPromises();
  await wrapper.findAll('input[type="text"]')[0]!.setValue(callsign);
  await wrapper.findAll('button').find((b) => b.text() === 'Save Changes')!.trigger('click');
  await flushPromises();
  return { wrapper, add };
}

describe('UserTable RadioID override', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
    useUserStore().id = 1;
  });

  it('asks before it saves a callsign that RadioID does not list', async () => {
    mockAPI.patch.mockRejectedValueOnce({
      response: { status: 400, data: { error: 'Callsign does not match DMR ID', radio_id_mismatch: true, radio_id_callsign: 'KI5VMF' } },
    });
    const { wrapper, add } = await saveCallsign('n0call');
    expect(add).not.toHaveBeenCalled();
    const prompt = wrapper.find('[data-test="user-radioid-prompt"]');
    expect(prompt.text()).toContain('RadioID lists 3191868 as KI5VMF.');
    expect(prompt.text()).toContain('Save N0CALL anyway');
    expect(wrapper.findAll('button').some((b) => b.text() === 'Save Changes')).toBe(false);

    mockAPI.patch.mockResolvedValueOnce({ data: { message: 'User updated' } });
    await wrapper.find('[data-test="user-radioid-override"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenLastCalledWith('/users/3191868', { callsign: 'n0call', username: 'testuser', override_radio_id: true });
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
    expect(wrapper.find('[data-test="user-radioid-prompt"]').exists()).toBe(false);
  });

  it('cancels the override', async () => {
    mockAPI.patch.mockRejectedValueOnce({
      response: { status: 400, data: { error: 'Callsign does not match DMR ID', radio_id_mismatch: true, radio_id_callsign: 'KI5VMF' } },
    });
    const { wrapper } = await saveCallsign('n0call');
    await wrapper.find('[data-test="user-radioid-prompt"]').findAll('button').find((b) => b.text() === 'Cancel')!.trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="user-radioid-prompt"]').exists()).toBe(false);
    expect(mockAPI.patch).toHaveBeenCalledTimes(1);
  });
});
