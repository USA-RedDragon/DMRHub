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

async function rename(username: string) {
  mockAPI.get.mockResolvedValue({ data: { total: 2, users: [row(1000001, 'N0CALL', 'alice'), row(1000002, 'N1CALL', 'bob')] } });
  const add = vi.fn();
  const wrapper = mount(UserTable, {
    attachTo: document.body,
    global: { mocks: { $toast: { add }, $confirm: { require: vi.fn() } } },
  });
  await flushPromises();
  const tr = () => wrapper.findAll('tr').find((r) => r.text().includes('N1CALL') || r.findAll('input').some((i) => (i.element as HTMLInputElement).value === 'N1CALL'));
  const edit = tr()!.findAll('button').find((b) => b.text() === 'Edit');
  await edit!.trigger('click');
  await flushPromises();
  const inputs = tr()!.findAll('input[type="text"]');
  await inputs[1]!.setValue(username);
  const save = tr()!.findAll('button').find((b) => b.text() === 'Save Changes');
  await save!.trigger('click');
  await flushPromises();
  return add;
}

describe('UserTable rename', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
    useUserStore().id = 1;
  });

  it('shows the error of the API', async () => {
    mockAPI.patch.mockRejectedValue({ response: { status: 400, data: { error: 'Username is the callsign of another user' } } });
    const add = await rename('n0call');
    expect(mockAPI.patch).toHaveBeenCalledWith('/users/1000002', { callsign: 'N1CALL', username: 'n0call' });
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', detail: 'Username is the callsign of another user' }));
  });

  it('shows a general error when the API gives none', async () => {
    mockAPI.patch.mockRejectedValue(new Error('network'));
    const add = await rename('n0call');
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', detail: 'Error updating user 1000002' }));
  });
});
