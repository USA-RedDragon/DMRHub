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
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
import AppsCard from '@/components/users/AppsCard.vue';

const mockAPI = API as unknown as { get: Mock; delete: Mock };

const apps = [
  {
    id: 'g1',
    origin: 'https://dmr-web.example',
    can_use: 'Talkgroups, last heard, messages, hotspots',
    allowed: '2026-10-23T12:00:00Z',
    last_used: '2026-10-23T12:30:00Z',
  },
  {
    id: 'g2',
    origin: 'https://ki5vmf.github.io',
    can_use: 'Talkgroups, last heard',
    allowed: '2026-09-30T12:00:00Z',
    last_used: '2026-10-20T12:00:00Z',
  },
];

async function mountCard() {
  const wrapper = mount(AppsCard, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('AppsCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('lists the apps that can use the account', async () => {
    mockAPI.get.mockResolvedValue({ data: apps });
    const wrapper = await mountCard();
    expect(mockAPI.get).toHaveBeenCalledWith('/oauth/apps');
    expect(wrapper.text()).toContain('Web apps that you allowed to use your DMRHub account. Removing one signs it out at once.');
    expect(wrapper.findAll('th').map((th) => th.text())).toEqual(['App', 'Can use', 'Allowed', 'Last used', 'Actions']);
    const rows = wrapper.findAll('tbody tr');
    expect(rows).toHaveLength(2);
    expect(rows[0].text()).toContain('https://dmr-web.example');
    expect(rows[0].text()).toContain('Talkgroups, last heard, messages, hotspots');
    expect(rows[1].text()).toContain('Sep 30, 2026');
  });

  it('says when no app can use the account', async () => {
    mockAPI.get.mockResolvedValue({ data: [] });
    const wrapper = await mountCard();
    expect(wrapper.text()).toContain('No apps can use your account.');
    expect(wrapper.find('table').exists()).toBe(false);
  });

  it('removes an app and says it is signed out', async () => {
    mockAPI.get.mockResolvedValue({ data: apps });
    mockAPI.delete.mockResolvedValue({ data: {} });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="app-g2"] button').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/oauth/apps/g2');
    expect(wrapper.findAll('tbody tr')).toHaveLength(1);
    expect(wrapper.text()).toContain('Removed https://ki5vmf.github.io. It is signed out.');
  });
});
