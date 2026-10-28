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

async function register(smtp: boolean, email?: string, add = vi.fn()) {
  mockAPI.get.mockResolvedValue({ data: { password_reset: smtp } });
  mockAPI.post.mockResolvedValue({ data: { message: 'ok' } });
  const wrapper = mount(RegisterPage, {
    global: { plugins: [createPinia()], mocks: { $toast: { add }, $router: { push: vi.fn() } } },
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

  it('shows the error of the API', async () => {
    const add = vi.fn();
    mockAPI.post.mockRejectedValueOnce({ response: { status: 400, data: { error: 'Username is the callsign of another user' } } });
    await register(false, undefined, add);
    expect(add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', detail: 'Username is the callsign of another user' }));
  });
});

describe('RegisterPage modes', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.location.hash = '';
  });

  const mountWith = async (registration: string) => {
    mockAPI.get.mockResolvedValue({ data: { registration } });
    mockAPI.post.mockResolvedValue({ data: { message: 'ok' } });
    const wrapper = mount(RegisterPage, {
      global: { plugins: [createPinia()], mocks: { $toast: { add: vi.fn() }, $router: { push: vi.fn() } } },
    });
    await flushPromises();
    return wrapper;
  };

  it('shows no form while registration is closed', async () => {
    const wrapper = await mountWith('closed');
    expect(wrapper.find('[data-test="register-closed"]').text()).toBe('Registration is closed.');
    expect(wrapper.find('form').exists()).toBe(false);
  });

  it('shows no form without an invite while registration needs one', async () => {
    const wrapper = await mountWith('invite');
    expect(wrapper.find('[data-test="register-invite-only"]').text()).toBe('Registration needs an invite.');
    expect(wrapper.find('form').exists()).toBe(false);
  });

  it('sends the invite of the link', async () => {
    window.location.hash = '#invite=4f1c0e9a7b2d43e8a1c5f60b9d7e2a13';
    const wrapper = await mountWith('invite');
    await wrapper.find('input#dmr_id').setValue('3100001');
    await wrapper.find('input#username').setValue('n0call');
    await wrapper.find('input#callsign').setValue('N0CALL');
    await wrapper.find('input#password').setValue('a long unique password');
    await wrapper.find('input#confirmPassword').setValue('a long unique password');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/users', expect.objectContaining({ invite: '4f1c0e9a7b2d43e8a1c5f60b9d7e2a13' }));
  });
});

