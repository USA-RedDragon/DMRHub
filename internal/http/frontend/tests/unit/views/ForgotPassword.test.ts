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
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

vi.mock('@/components/ui/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import API from '@/services/API';
import LoginPage from '@/views/auth/LoginPage.vue';
import ForgotPasswordPage from '@/views/auth/ForgotPasswordPage.vue';
import ResetPasswordPage from '@/views/auth/ResetPasswordPage.vue';
import { routes } from '@/router/routes';

const mockAPI = API as unknown as { get: Mock; post: Mock };

const sent = 'If that account has an email address, a link is on its way. It works for 1 hour.';
const badLink = 'This link does not work. It is older than 1 hour or was used already. Ask for a new one.';
const token = 'a'.repeat(64);

async function mountLogin(passwordReset: boolean) {
  mockAPI.get.mockResolvedValue({ data: { ipsc: false, openbridge: false, password_reset: passwordReset } });
  const wrapper = mount(LoginPage, {
    global: {
      mocks: { $router: { push: vi.fn() }, $route: { query: {} }, $toast: { add: vi.fn() } },
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return wrapper;
}

async function mountReset(hash: string) {
  window.history.replaceState(null, '', `/reset-password${hash}`);
  const wrapper = mount(ResetPasswordPage);
  await flushPromises();
  return wrapper;
}

describe('forgot password', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
  });

  it('has its two pages', () => {
    const paths = routes().map((r) => r.path);
    expect(paths).toContain('/forgot-password');
    expect(paths).toContain('/reset-password');
  });

  it('links from the login page when email is on', async () => {
    const wrapper = await mountLogin(true);
    expect(mockAPI.get).toHaveBeenCalledWith('/network/features');
    const link = wrapper.find('[data-test="forgot-password"]');
    expect(link.text()).toBe('Forgot your password?');
    expect(link.attributes('href')).toBe('/forgot-password');
  });

  it('has no link on the login page when email is off', async () => {
    const wrapper = await mountLogin(false);
    expect(wrapper.find('[data-test="forgot-password"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('Forgot');
  });

  it('asks for a link by username or callsign', async () => {
    mockAPI.post.mockResolvedValue({ data: { message: sent } });
    const wrapper = mount(ForgotPasswordPage);
    const card = wrapper.find('[data-test="forgot-password-card"]');
    expect(card.text()).toContain('Forgot your password?');
    expect(card.text()).toContain('DMRHub emails you a link to choose a new one.');
    expect(wrapper.find('label[for="forgot-user"]').text()).toBe('Username or callsign');
    expect(wrapper.find('button[type="submit"]').text()).toBe('Email me a link');
    expect(wrapper.find('[data-test="forgot-sent"]').exists()).toBe(false);

    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).not.toHaveBeenCalled();

    await wrapper.find('#forgot-user').setValue(' n0call ');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/auth/forgot-password', { user: 'n0call' });
    expect(wrapper.find('[data-test="forgot-sent"]').text()).toBe(sent);
  });

  it('names the account of the link and takes the token out of the address', async () => {
    mockAPI.post.mockResolvedValue({ data: { callsign: 'N0CALL' } });
    const wrapper = await mountReset(`#token=${token}`);
    expect(mockAPI.post).toHaveBeenCalledWith('/auth/reset-password/check', { token });
    expect(window.location.hash).toBe('');
    expect(window.location.pathname).toBe('/reset-password');
    const card = wrapper.find('[data-test="reset-password-card"]');
    expect(card.text()).toContain('Choose a new password');
    expect(card.text()).toContain('For N0CALL.');
    expect(wrapper.find('label[for="reset-new"]').text()).toBe('New password');
    expect(wrapper.find('label[for="reset-confirm"]').text()).toBe('Confirm new password');
    expect(wrapper.find('button[type="submit"]').text()).toBe('Set password');
  });

  it('checks the new password and sets it', async () => {
    mockAPI.post.mockResolvedValueOnce({ data: { callsign: 'N0CALL' } });
    const wrapper = await mountReset(`#token=${token}`);

    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="reset-error"]').text()).toBe('Enter a new password.');
    await wrapper.find('#reset-new').setValue('my-new-password');
    await wrapper.find('#reset-confirm').setValue('my-other-password');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="reset-error"]').text()).toBe('The new passwords do not match.');
    expect(mockAPI.post).toHaveBeenCalledTimes(1);

    mockAPI.post.mockResolvedValueOnce({ data: { message: 'Your password is set. Log in with it now.' } });
    await wrapper.find('#reset-confirm').setValue('my-new-password');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenLastCalledWith('/auth/reset-password', { token, password: 'my-new-password' });
    expect(wrapper.find('[data-test="reset-done"]').text()).toBe('Your password is set. Log in with it now.');
    expect(wrapper.find('[data-test="reset-error"]').exists()).toBe(false);
  });

  it('shows the error of the server for a breached password', async () => {
    mockAPI.post.mockResolvedValueOnce({ data: { callsign: 'N0CALL' } });
    const wrapper = await mountReset(`#token=${token}`);
    mockAPI.post.mockRejectedValueOnce({
      response: { status: 400, data: { error: 'Password has been reported in a data breach. Please use another one' } },
    });
    await wrapper.find('#reset-new').setValue('password');
    await wrapper.find('#reset-confirm').setValue('password');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="reset-error"]').text()).toBe(
      'Password has been reported in a data breach. Please use another one',
    );
    expect(wrapper.find('[data-test="reset-done"]').exists()).toBe(false);
  });

  it('says when a link does not work', async () => {
    mockAPI.post.mockRejectedValue({ response: { status: 400, data: { error: badLink } } });
    let wrapper = await mountReset(`#token=${token}`);
    expect(wrapper.find('[data-test="reset-error"]').text()).toBe(badLink);
    expect(wrapper.find('form').exists()).toBe(false);

    mockAPI.post.mockClear();
    wrapper = await mountReset('');
    expect(mockAPI.post).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="reset-error"]').text()).toBe(badLink);
  });
});
