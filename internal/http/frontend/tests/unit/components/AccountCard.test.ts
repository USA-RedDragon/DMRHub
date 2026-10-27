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
  default: { get: vi.fn(), patch: vi.fn() },
}));

import API from '@/services/API';
import AccountCard from '@/components/users/AccountCard.vue';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

async function card(opts: { validation: boolean; listed: string; callsign?: string }) {
  const pinia = createPinia();
  setActivePinia(pinia);
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/network/features') {
      return Promise.resolve({ data: { radio_id_validation: opts.validation } });
    }
    if (url === '/userdb/3191868') {
      return opts.listed ? Promise.resolve({ data: { callsign: opts.listed } }) : Promise.reject(new Error('404'));
    }
    return Promise.reject(new Error(url));
  });
  const wrapper = mount(AccountCard, {
    props: { id: 3191868, username: 'testuser', callsign: opts.callsign ?? 'OLDCALL' },
    global: { plugins: [pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe('AccountCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the DMR ID read only and the username and callsign to edit', async () => {
    const wrapper = await card({ validation: true, listed: 'OLDCALL' });
    expect((wrapper.find('input#account-dmr-id').element as HTMLInputElement).value).toBe('3191868');
    expect(wrapper.find('input#account-dmr-id').attributes('readonly')).toBeDefined();
    expect((wrapper.find('input#account-username').element as HTMLInputElement).value).toBe('testuser');
    expect((wrapper.find('input#account-callsign').element as HTMLInputElement).value).toBe('OLDCALL');
    expect(wrapper.find('[data-test="account-radioid"]').exists()).toBe(false);
  });

  it('offers the callsign that RadioID lists when it differs', async () => {
    const wrapper = await card({ validation: true, listed: 'ki5vmf' });
    expect(wrapper.find('[data-test="account-radioid"]').text()).toContain('RadioID lists DMR ID 3191868 as KI5VMF.');
    await wrapper.find('[data-test="account-use-radioid"]').trigger('click');
    expect((wrapper.find('input#account-callsign').element as HTMLInputElement).value).toBe('KI5VMF');
    mockAPI.patch.mockResolvedValue({ data: { message: 'User updated' } });
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/users/3191868', { username: 'testuser', callsign: 'KI5VMF' });
    expect(wrapper.find('[data-test="account-ok"]').text()).toBe('Saved');
    expect(wrapper.find('[data-test="account-radioid"]').exists()).toBe(false);
  });

  it('hides the RadioID hint when radio ID validation is off', async () => {
    const wrapper = await card({ validation: false, listed: 'KI5VMF' });
    expect(wrapper.find('[data-test="account-radioid"]').exists()).toBe(false);
  });

  it('shows the server error under the callsign', async () => {
    const wrapper = await card({ validation: true, listed: 'KI5VMF' });
    mockAPI.patch.mockRejectedValue({ response: { data: { error: 'Callsign does not match DMR ID' } } });
    await wrapper.find('input#account-callsign').setValue('n0call');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/users/3191868', { username: 'testuser', callsign: 'N0CALL' });
    expect(wrapper.find('[data-test="account-error"]').text()).toBe('Callsign does not match DMR ID');
  });
});
