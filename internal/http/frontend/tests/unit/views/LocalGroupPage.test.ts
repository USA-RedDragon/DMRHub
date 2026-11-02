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
  default: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
}));

import API from '@/services/API';
import LocalGroupPage from '@/views/repeaters/LocalGroupPage.vue';
import RepeatersPage from '@/views/repeaters/RepeatersPage.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; post: Mock; patch: Mock; delete: Mock };

const owner = { id: 3191868, callsign: 'W5DFW' };
const group = (extra: Record<string, unknown> = {}) => ({
  id: 5, name: 'North Texas', talkgroup: 8, owner, members: 2, mine: true, can_manage: true, involved: true,
  repeaters: [
    { id: 311888, callsign: 'W5DFW', owner, slot: 2, state: 'member', mine: true },
    { id: 319186801, callsign: 'KP4DJT', owner: { id: 3191869, callsign: 'KP4DJT' }, slot: 2, state: 'member', mine: false },
    { id: 311902, callsign: 'N5ARL', owner: { id: 3191870, callsign: 'N5ARL' }, slot: 1, state: 'asked', mine: false },
  ],
  ...extra,
});

function serve(g: Record<string, unknown>) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/localgroups') {
      return Promise.resolve({ data: { groups: [g], talkgroups: [8, 9] } });
    }
    return Promise.resolve({ data: g });
  });
}

async function mountGroup(id: string) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useUserStore().loggedIn = true;
  const push = vi.fn();
  const wrapper = mount(LocalGroupPage, {
    global: {
      plugins: [pinia],
      mocks: { $route: { params: { id } }, $router: { push } },
      stubs: { RouterLink: true },
    },
  });
  await flushPromises();
  return { wrapper, push };
}

describe('LocalGroupPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the group, its repeaters and the one that waits for its owner', async () => {
    serve(group());
    const { wrapper } = await mountGroup('5');
    expect(wrapper.find('[data-test="group-card"]').text()).toContain('A call to TG 8 on one of these repeaters goes to all of them, and nowhere else.');
    expect((wrapper.find('[data-test="group-name"]').element as HTMLInputElement).value).toBe('North Texas');
    expect(wrapper.find('[data-test="member-311888"]').text()).toContain('W5DFW — 311888');
    expect(wrapper.find('[data-test="member-311888"]').text()).toContain('You');
    expect(wrapper.find('[data-test="member-311902"]').text()).toContain('Waiting for the owner');
    expect(wrapper.find('[data-test="member-319186801"] [data-test="waiting"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="group-delete"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('adds a repeater by its ID on TS2 and refuses a bad ID', async () => {
    serve(group());
    mockAPI.post.mockResolvedValue({ data: group() });
    const { wrapper } = await mountGroup('5');
    await wrapper.find('[data-test="add-repeater"]').setValue('12');
    await wrapper.find('form').trigger('submit');
    expect(wrapper.find('[data-test="add-error"]').text()).toBe('Type the 6 to 9 digit ID of a repeater or hotspot');
    expect(mockAPI.post).not.toHaveBeenCalled();
    await wrapper.find('[data-test="add-repeater"]').setValue('311999');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/localgroups/5/repeaters', { repeater_id: 311999, slot: 2 });
    expect(wrapper.find('[data-test="add-error"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('shows why the hub refused a repeater', async () => {
    serve(group());
    mockAPI.post.mockRejectedValue({ response: { status: 409, data: { error: 'This repeater is already in a local group on that talkgroup' } } });
    const { wrapper } = await mountGroup('5');
    await wrapper.find('[data-test="add-repeater"]').setValue('311999');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="add-error"]').text()).toBe('This repeater is already in a local group on that talkgroup');
    wrapper.unmount();
  });

  it('removes a repeater and deletes the group after a second click', async () => {
    serve(group());
    mockAPI.delete.mockResolvedValue({ data: {} });
    const { wrapper, push } = await mountGroup('5');
    await wrapper.find('[data-test="remove-311902"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/localgroups/5/repeaters/311902');
    await wrapper.find('[data-test="group-delete-ask"]').trigger('click');
    expect(mockAPI.delete).toHaveBeenCalledTimes(1);
    await wrapper.find('[data-test="group-delete-confirm"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenLastCalledWith('/localgroups/5');
    expect(push).toHaveBeenCalledWith('/repeaters?view=local');
    wrapper.unmount();
  });

  it('lets the owner of a member repeater only take out that repeater', async () => {
    serve(group({ mine: false, can_manage: false, owner: { id: 3191870, callsign: 'N5ARL' } }));
    const { wrapper } = await mountGroup('5');
    expect(wrapper.find('[data-test="group-name"]').attributes('disabled')).toBeDefined();
    expect(wrapper.find('[data-test="group-save"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="add-repeater"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="group-delete"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="remove-311888"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="remove-319186801"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('makes a new group on the first local talkgroup and opens it', async () => {
    serve(group());
    mockAPI.post.mockResolvedValue({ data: group({ id: 7, name: 'Club hotspots', repeaters: [] }) });
    const { wrapper, push } = await mountGroup('new');
    expect(wrapper.find('[data-test="group-card"]').text()).toContain('New local group');
    expect(wrapper.find('[data-test="group-repeaters"]').exists()).toBe(false);
    await wrapper.find('[data-test="group-name"]').setValue('Club hotspots');
    await wrapper.find('[data-test="group-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/localgroups', { name: 'Club hotspots', talkgroup: 8 });
    expect(push).toHaveBeenCalledWith('/localgroups/7');
    wrapper.unmount();
  });
});

describe('RepeatersPage local groups', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lists the local groups with their talkgroup, size and owner', async () => {
    mockAPI.get.mockImplementation((url: string) => {
      if (url === '/localgroups') {
        return Promise.resolve({
          data: {
            groups: [group(), group({ id: 6, name: 'Club hotspots', talkgroup: 9, members: 2, mine: false, owner: { id: 3191871, callsign: 'KI5VMF' } })],
            talkgroups: [8, 9],
          },
        });
      }
      return Promise.resolve({ data: { total: 0, repeaters: [], talkgroups: [] } });
    });
    const pinia = createPinia();
    setActivePinia(pinia);
    useUserStore().loggedIn = true;
    const replace = vi.fn();
    const wrapper = mount(RepeatersPage, {
      global: {
        plugins: [pinia],
        mocks: { $route: { query: { view: 'local' } }, $router: { replace } },
        stubs: { RouterLink: { template: '<a><slot /></a>' }, RepeaterTable: true },
      },
    });
    await flushPromises();
    const rows = wrapper.findAll('[data-test="local-groups"] tbody tr').map((r) => r.text());
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatch(/North Texas\s*TG 8\s*2\s*You/);
    expect(rows[1]).toMatch(/Club hotspots\s*TG 9\s*2\s*KI5VMF/);
    expect(wrapper.find('[data-test="new-local-group"]').exists()).toBe(true);
    await wrapper.findAll('input[type="radio"]')[0]!.setValue(true);
    expect(replace).toHaveBeenCalledWith({ query: {} });
    wrapper.unmount();
  });
});
