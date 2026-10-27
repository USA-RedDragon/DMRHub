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
import RegisterPage from '@/views/auth/RegisterPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

async function register(smtp: boolean, email?: string) {
  mockAPI.get.mockResolvedValue({ data: { password_reset: smtp } });
  mockAPI.post.mockResolvedValue({ data: { message: 'ok' } });
  const wrapper = mount(RegisterPage, {
    global: { plugins: [createPinia()], mocks: { $toast: { add: vi.fn() }, $router: { push: vi.fn() } } },
  });
  await flushPromises();
  await wrapper.find('input#dmr_id').setValue('3100001');
  await wrapper.find('input#username').setValue('n0call');
  await wrapper.find('input#callsign').setValue('N0CALL');
  if (email !== undefined) {
    await wrapper.find('input#email').setValue(email);
  }
  await wrapper.find('input#password').setValue('a long unique password');
  await wrapper.find('input#confirmPassword').setValue('a long unique password');
  await wrapper.find('form').trigger('submit');
  await flushPromises();
  return wrapper;
}

describe('RegisterPage email', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sends the optional email when the network can send email', async () => {
    await register(true, 'n0call@example.com');
    expect(mockAPI.post).toHaveBeenCalledWith('/users', expect.objectContaining({ email: 'n0call@example.com' }));
  });

  it('leaves out an empty email', async () => {
    await register(true, '');
    expect(mockAPI.post.mock.calls[0]![1]).not.toHaveProperty('email');
  });

  it('hides the email field and says a reset needs an admin without SMTP', async () => {
    const wrapper = await register(false);
    expect(wrapper.find('input#email').exists()).toBe(false);
    expect(wrapper.find('[data-test="no-email"]').text()).toBe('This network cannot send email. To reset a forgotten password, ask an admin.');
    expect(mockAPI.post.mock.calls[0]![1]).not.toHaveProperty('email');
  });
});
