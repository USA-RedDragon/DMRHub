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
import type { Invite } from '@/services/accounts';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import InvitesPage from '@/views/admin/InvitesPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; delete: Mock };

const future = new Date(Date.now() + 86400000).toISOString();
const past = new Date(Date.now() - 86400000).toISOString();

function invite(id: number, over: Partial<Invite> = {}): Invite {
  return { id, created_by: 1, created_by_callsign: 'N0ADM', created_at: past, expires_at: future, used_by: null, used_at: null, ...over };
}

function serve(invites: Invite[]) {
  mockAPI.get.mockImplementation((url: string) => (url === '/admin/invites' ? Promise.resolve({ data: { invites } }) : Promise.reject(new Error(url))));
}

async function page() {
  const wrapper = mount(InvitesPage, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('InvitesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('lists the invites with Delete only on the unused ones', async () => {
    serve([
      invite(1, { used_by: 3191868, used_by_callsign: 'KI5VMF', used_at: past }),
      invite(2),
      invite(3, { expires_at: past }),
    ]);
    const wrapper = await page();
    const rows = wrapper.findAll('[data-test="invite-row"]');
    expect(rows).toHaveLength(3);
    expect(rows[0]!.text()).toContain('Used');
    expect(rows[0]!.text()).toContain('KI5VMF');
    expect(rows[0]!.find('[data-test="invite-delete"]').exists()).toBe(false);
    expect(rows[1]!.find('[data-test="invite-delete"]').exists()).toBe(true);
    expect(rows[2]!.text()).toContain('Expired');
    expect(rows[2]!.find('[data-test="invite-delete"]').exists()).toBe(true);
  });

  it('shows a new invite link once', async () => {
    serve([]);
    const wrapper = await page();
    expect(wrapper.find('[data-test="invite-empty"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="invite-once"]').exists()).toBe(false);
    const link = 'https://dmrhub.example.org/register#invite=4f1c0e9a7b2d43e8a1c5f60b9d7e2a13';
    mockAPI.post.mockResolvedValue({ data: { ...invite(4), code: '4f1c0e9a7b2d43e8a1c5f60b9d7e2a13', link } });
    serve([invite(4)]);
    await wrapper.find('[data-test="invite-new"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/admin/invites');
    expect((wrapper.find('[data-test="invite-once"] input').element as HTMLInputElement).value).toBe(link);
    expect(wrapper.find('[data-test="invite-once"]').text()).toContain('Shown once. Works once, for 7 days.');
    expect(wrapper.findAll('[data-test="invite-row"]')).toHaveLength(1);
  });

  it('deletes an unused invite', async () => {
    serve([invite(2)]);
    const wrapper = await page();
    mockAPI.delete.mockResolvedValue({ data: {} });
    serve([]);
    await wrapper.find('[data-test="invite-delete"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/admin/invites/2');
    expect(wrapper.find('[data-test="invite-empty"]').exists()).toBe(true);
  });
});
