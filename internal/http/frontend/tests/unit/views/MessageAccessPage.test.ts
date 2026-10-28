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
import { accessWhat, type MessageAccess } from '@/services/accounts';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn() },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import MessageAccessPage from '@/views/admin/MessageAccessPage.vue';

const mockAPI = API as unknown as { get: Mock };

function access(id: number, over: Partial<MessageAccess> = {}): MessageAccess {
  return {
    id, at: '2026-10-27T19:03:00Z', admin_id: 1, admin_callsign: 'N0ADM', kind: 'search', message_id: null,
    filter: 'src=3191868', results: 25,
    radio_ids: [{ radio_id: 3191868, user_id: 3191868, callsign: 'KI5VMF' }, { radio_id: 3100, user_id: null }],
    ...over,
  };
}

function serve(accesses: MessageAccess[]) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/admin/message-access?')) {
      return Promise.resolve({ data: { total: accesses.length, accesses } });
    }
    if (url.startsWith('/users?')) {
      return Promise.resolve({ data: { users: [{ id: 1, callsign: 'N0ADM', admin: true, superAdmin: true }, { id: 2, callsign: 'N0USR', admin: false, superAdmin: false }] } });
    }
    return Promise.reject(new Error(url));
  });
}

describe('MessageAccessPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lists who saw whose messages', async () => {
    serve([access(2, { kind: 'read', message_id: 4182, filter: '', results: 1 }), access(1)]);
    const wrapper = mount(MessageAccessPage);
    await flushPromises();
    const rows = wrapper.findAll('[data-test="access-row"]');
    expect(rows).toHaveLength(2);
    expect(rows[0]!.text()).toContain('Message 4182');
    expect(rows[1]!.text()).toContain('Search: from 3191868');
    expect(rows[1]!.text()).toContain('N0ADM');
    const ids = rows[1]!.findAll('[data-test="access-radio"]').map((r) => r.text());
    expect(ids).toEqual(['3191868KI5VMF', '3100']);
    const admins = wrapper.findAll('[data-test="access-admin"] option').map((o) => o.text());
    expect(admins).toEqual(['All', 'N0ADM']);
  });

  it('filters by radio ID and admin', async () => {
    serve([]);
    const wrapper = mount(MessageAccessPage);
    await flushPromises();
    expect(wrapper.find('[data-test="access-empty"]').text()).toBe('None');
    await wrapper.find('[data-test="access-radio-id"]').setValue('3191868');
    await wrapper.find('[data-test="access-radio-id"]').trigger('change');
    await wrapper.find('[data-test="access-admin"]').setValue('1');
    await flushPromises();
    const urls = mockAPI.get.mock.calls.map((c) => c[0] as string).filter((u) => u.startsWith('/admin/message-access?'));
    expect(urls.at(-1)).toBe('/admin/message-access?page=1&limit=50&radio_id=3191868&admin=1');
  });

  it('names a search by its filters', () => {
    expect(accessWhat(access(1, { filter: 'dst=3100&text=net&page=2' }))).toBe('Search: to 3100, text "net"');
    expect(accessWhat(access(1, { filter: '' }))).toBe('Search');
  });
});
