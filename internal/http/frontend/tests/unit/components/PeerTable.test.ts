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
  default: {
    get: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
import PeerTable from '@/components/PeerTable.vue';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

const peer = (enhanced: boolean) => ({
  id: 500001,
  ingress: true,
  egress: true,
  enhanced,
  created_at: '2026-10-22T22:21:05Z',
  last_ping_time: '0001-01-01T00:00:00Z',
});

async function mountTable(admin: boolean, enhanced: boolean) {
  mockAPI.get.mockResolvedValue({ data: { total: 1, peers: [peer(enhanced)] } });
  const toast = { add: vi.fn() };
  const wrapper = mount(PeerTable, {
    props: { admin },
    global: {
      mocks: { $toast: toast, $confirm: { require: vi.fn() } },
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return { wrapper, toast };
}

describe('PeerTable', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lets an admin turn on enhanced OpenBridge for a peer', async () => {
    mockAPI.patch.mockResolvedValue({ data: peer(true) });
    const { wrapper, toast } = await mountTable(true, false);
    const box = wrapper.find('input#enhanced');
    expect(box.exists()).toBe(true);
    expect((box.element as HTMLInputElement).disabled).toBe(false);
    await box.setValue(true);
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/500001', { enhanced: true });
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
  });

  it('shows the setting to an owner who is not an admin, without letting them change it', async () => {
    const { wrapper } = await mountTable(false, true);
    const box = wrapper.find('input#enhanced');
    expect((box.element as HTMLInputElement).checked).toBe(true);
    expect((box.element as HTMLInputElement).disabled).toBe(true);
  });
});
