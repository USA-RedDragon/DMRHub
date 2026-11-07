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

import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

import API from '@/services/API';
import NewRepeaterPage from '@/views/repeaters/NewRepeaterPage.vue';
import { takeSecret } from '@/lib/enroll';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; post: Mock };

const users = [
  { id: 9990001, callsign: 'N0CALL', username: 'n0call', approved: true, suspended: false },
  { id: 9990002, callsign: 'N1CALL', username: 'n1call', approved: true, suspended: false },
  { id: 9990003, callsign: 'N2CALL', username: 'n2call', approved: true, suspended: true },
];

async function mountPage(features: Record<string, unknown>, me?: { id: number; admin: boolean }) {
  mockAPI.get.mockImplementation((path: string) => Promise.resolve({ data: path.startsWith('/users') ? { total: users.length, users } : features }));
  const toast = { add: vi.fn() };
  const router = { push: vi.fn() };
  const pinia = createPinia();
  if (me) {
    const store = useUserStore(pinia);
    store.loggedIn = true;
    store.id = me.id;
    store.admin = me.admin;
  }
  const wrapper = mount(NewRepeaterPage, {
    global: {
      plugins: [pinia],
      mocks: { $toast: toast, $router: router },
    },
  });
  await flushPromises();
  return { wrapper, toast, router };
}

describe('NewRepeaterPage', () => {
  let alert: Mock;

  beforeEach(() => {
    vi.clearAllMocks();
    alert = vi.fn();
    vi.spyOn(window, 'alert').mockImplementation(alert);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('opens the connect page with the password of a new hotspot, once', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'Ab3dE6gH9jK2' } });
    const { wrapper, router } = await mountPage({ ipsc: false, mmdvm_port: 62040, ipsc_port: 50010 });
    await wrapper.find('input#radioID').setValue('300240101');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters', { id: 300240101, type: 'mmdvm', simplex_repeater: false });
    expect(router.push).toHaveBeenCalledWith('/repeaters/300240101/connect');
    expect(alert).not.toHaveBeenCalled();
    expect(takeSecret(300240101)).toBe('Ab3dE6gH9jK2');
    expect(takeSecret(300240101)).toBeUndefined();
  });

  it('opens the connect page with the auth key of a new IPSC repeater', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'abcd' } });
    const { wrapper, router } = await mountPage({ ipsc: true, mmdvm_port: 62040, ipsc_port: 50010 });
    await wrapper.find('select#repeaterType').setValue('ipsc');
    await wrapper.find('input#radioID').setValue('990001');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(router.push).toHaveBeenCalledWith('/repeaters/990001/connect');
    expect(takeSecret(990001)).toBe('abcd');
  });

  it('says the repeater could not be created when the API refuses it', async () => {
    mockAPI.post.mockRejectedValue(new Error('network'));
    const { wrapper, toast, router } = await mountPage({ ipsc: false, mmdvm_port: 62031, ipsc_port: 50000 });
    await wrapper.find('input#radioID').setValue('300240101');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ detail: 'Error creating repeater' }));
    expect(router.push).not.toHaveBeenCalled();
  });

  it('shows no Owner field to a user who is not an admin', async () => {
    const { wrapper } = await mountPage({ ipsc: false }, { id: 9990002, admin: false });
    expect(wrapper.find('[data-test="owner-field"]').exists()).toBe(false);
    expect(mockAPI.get).not.toHaveBeenCalledWith('/users?limit=none');
  });

  it('enrolls for the admin by default and for the user the admin picks', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'Ab3dE6gH9jK2' } });
    const { wrapper } = await mountPage({ ipsc: false }, { id: 9990002, admin: true });
    const rows = wrapper.findAll('[data-test="owner-user"]');
    expect(rows.map((r) => r.find('b').text())).toEqual(['N1CALL', 'N0CALL']);
    expect((rows[0]!.find('input').element as HTMLInputElement).checked).toBe(true);
    await wrapper.find('input#radioID').setValue('999000201');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenLastCalledWith('/repeaters', { id: 999000201, type: 'mmdvm', simplex_repeater: false, owner: 9990002 });

    await rows[1]!.find('input').setValue(true);
    await wrapper.find('input#radioID').setValue('999000101');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenLastCalledWith('/repeaters', { id: 999000101, type: 'mmdvm', simplex_repeater: false, owner: 9990001 });
  });
});
