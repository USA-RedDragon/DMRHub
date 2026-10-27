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
import { createPinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}));

import API from '@/services/API';
import InitialUserPage from '@/views/setup/InitialUserPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

async function createAdmin() {
  mockAPI.get.mockResolvedValue({ data: {} });
  const wrapper = mount(InitialUserPage, {
    global: { plugins: [createPinia()], mocks: { $toast: { add: vi.fn() }, $route: { query: {} }, $router: { push: vi.fn() } } },
  });
  await flushPromises();
  await wrapper.find('input#dmr_id').setValue('3000001');
  await wrapper.find('input#username').setValue('admin');
  await wrapper.find('input#callsign').setValue('N0CALL');
  await wrapper.find('input#password').setValue('a long unique password');
  await wrapper.find('input#confirmPassword').setValue('a long unique password');
  await wrapper.find('form').trigger('submit');
  await flushPromises();
  return wrapper;
}

describe('InitialUserPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('says that the first visitor becomes the admin', async () => {
    mockAPI.post.mockResolvedValue({ data: { message: 'ok' } });
    const wrapper = await createAdmin();
    expect(wrapper.text()).toContain('Create the first admin');
    expect(wrapper.find('[data-test="first-admin-banner"]').text()).toContain('Whoever creates this account first becomes the admin.');
    expect(wrapper.find('[data-test="first-admin-banner"]').text()).toContain('finish setup before you expose DMRHub to the internet');
  });

  it('makes the admin and ends the wizard with no token', async () => {
    mockAPI.post.mockResolvedValue({ data: { message: 'ok' } });
    await createAdmin();
    expect(mockAPI.post).toHaveBeenCalledWith('/users', expect.objectContaining({ username: 'admin' }));
    expect(mockAPI.post).toHaveBeenCalledWith('/setupwizard/complete', {});
    for (const call of mockAPI.post.mock.calls) {
      expect(call).toHaveLength(2);
    }
  });

  it('ends the wizard when another visitor made the admin first', async () => {
    mockAPI.post.mockImplementation((path: string) => path === '/users'
      ? Promise.reject({ response: { status: 409, data: { error: 'Someone else made the first admin already. Log in with that account.' } } })
      : Promise.resolve({ data: {} }));
    await createAdmin();
    expect(mockAPI.post).toHaveBeenCalledWith('/setupwizard/complete', {});
  });
});
