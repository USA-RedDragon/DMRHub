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
import type { UnassignedRepeater } from '@/services/repeater';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import { useAdminStore } from '@/store';
import UnassignedRepeatersPage from '@/views/admin/UnassignedRepeatersPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

function repeater(id: number, over: Partial<UnassignedRepeater> = {}): UnassignedRepeater {
  return {
    id,
    type: 'modem',
    callsign: 'N6CALL',
    rx_frequency: 438800000,
    tx_frequency: 438800000,
    color_code: 1,
    slots: 4,
    connected_time: new Date(Date.now() - 12 * 60 * 1000).toISOString(),
    created_at: '2026-10-24T15:00:00Z',
    ...over,
  };
}

const users = [
  { id: 3002068, callsign: 'N6CALL', username: 'jacob', approved: true, suspended: false },
  { id: 3003345, callsign: 'W2CALL', username: 'ann', approved: true, suspended: false },
  { id: 3004167, callsign: 'N7TEST', username: 'sam', approved: true, suspended: true },
];

let list: UnassignedRepeater[] = [];

function serve() {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/repeaters/unassigned?')) {
      return Promise.resolve({ data: { total: list.length, repeaters: list } });
    }
    if (url === '/users?limit=none') {
      return Promise.resolve({ data: { total: users.length, users } });
    }
    return Promise.reject(new Error(url));
  });
}

async function page() {
  const pinia = createPinia();
  setActivePinia(pinia);
  const wrapper = mount(UnassignedRepeatersPage, {
    global: { plugins: [pinia] },
    attachTo: document.body,
  });
  await flushPromises();
  return wrapper;
}

const dialog = () => document.querySelector('[data-test="unassigned-dialog"]');
const userRows = () => [...document.querySelectorAll('[data-test="unassigned-user"]')];

describe('UnassignedRepeatersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    list = [];
    serve();
  });

  it('lists the repeaters with no owner', async () => {
    list = [
      repeater(990360),
      repeater(990361, { callsign: '', rx_frequency: 444125000, tx_frequency: 449125000, color_code: 3, slots: 3, connected_time: '0001-01-01T00:00:00Z' }),
    ];
    const wrapper = await page();
    const rows = wrapper.findAll('[data-test="unassigned-row"]');
    expect(rows).toHaveLength(2);
    expect(rows[0]!.text()).toContain('990360');
    expect(rows[0]!.text()).toContain('Modem, hotspot');
    expect(rows[0]!.text()).toContain('N6CALL');
    expect(rows[0]!.text()).toContain('438.8000 MHz');
    expect(rows[0]!.text()).toContain('12 minutes ago');
    expect(rows[0]!.text()).toContain('Oct 24, 2026');
    expect(rows[1]!.text()).toContain('Modem, repeater');
    expect(rows[1]!.text()).toContain('None');
    expect(rows[1]!.text()).toContain('RX 444.1250 / TX 449.1250 MHz');
    expect(rows[1]!.text()).toContain('Never');
    expect(useAdminStore().unassignedRepeaters).toBe(2);
  });

  it('says so when every repeater has an owner', async () => {
    const wrapper = await page();
    expect(wrapper.find('[data-test="unassigned-empty"]').text()).toBe('No unassigned repeaters');
    expect(useAdminStore().unassignedRepeaters).toBe(0);
  });

  it('finds a user by callsign, username or DMR ID, and leaves out suspended users', async () => {
    list = [repeater(990360)];
    const wrapper = await page();
    await wrapper.find('[data-test="unassigned-assign"]').trigger('click');
    await flushPromises();
    expect(dialog()?.textContent).toContain('Assign an owner to repeater 990360');
    expect(userRows()).toHaveLength(2);
    const search = document.querySelector('[data-test="unassigned-search"]') as HTMLInputElement;
    for (const [q, want] of [['w2c', 'W2CALL'], ['jac', 'N6CALL'], ['30033', 'W2CALL']] as const) {
      search.value = q;
      search.dispatchEvent(new Event('input'));
      await flushPromises();
      expect(userRows().map((r) => r.textContent)).toEqual([expect.stringContaining(want)]);
    }
    search.value = 'N7TEST';
    search.dispatchEvent(new Event('input'));
    await flushPromises();
    expect(document.querySelector('[data-test="unassigned-nomatch"]')?.textContent).toBe('No user matches');
  });

  it('assigns the picked user and drops the repeater from the list', async () => {
    list = [repeater(990360)];
    const wrapper = await page();
    await wrapper.find('[data-test="unassigned-assign"]').trigger('click');
    await flushPromises();
    const confirm = document.querySelector('[data-test="unassigned-confirm"]') as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    (userRows()[1]!.querySelector('input') as HTMLInputElement).click();
    await flushPromises();
    expect(confirm.disabled).toBe(false);
    mockAPI.post.mockResolvedValue({ data: { message: 'Owner assigned' } });
    list = [];
    confirm.click();
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/990360/owner', { owner: 3003345 });
    expect(wrapper.find('[data-test="unassigned-ok"]').text()).toBe('W2CALL now owns repeater 990360.');
    expect(wrapper.find('[data-test="unassigned-empty"]').exists()).toBe(true);
    expect(dialog()).toBeNull();
    expect(useAdminStore().unassignedRepeaters).toBe(0);
  });

  it('shows the error of a repeater that got an owner meanwhile', async () => {
    list = [repeater(990360)];
    const wrapper = await page();
    await wrapper.find('[data-test="unassigned-assign"]').trigger('click');
    await flushPromises();
    (userRows()[0]!.querySelector('input') as HTMLInputElement).click();
    await flushPromises();
    mockAPI.post.mockRejectedValue({ response: { status: 409, data: { error: 'Repeater already has an owner' } } });
    list = [];
    (document.querySelector('[data-test="unassigned-confirm"]') as HTMLButtonElement).click();
    await flushPromises();
    expect(wrapper.find('[data-test="unassigned-error"]').text()).toBe('Repeater already has an owner');
    expect(wrapper.findAll('[data-test="unassigned-row"]')).toHaveLength(0);
  });
});
