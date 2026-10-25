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
import { createPinia, setActivePinia } from 'pinia';
import type { RouteLocationNormalized } from 'vue-router';

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

const mustChangeError = {
  response: {
    status: 403,
    data: {
      error: 'An admin set a temporary password for you. Choose your own password in Settings first.',
      must_change_password: true,
    },
  },
};

const banner = 'An admin set a temporary password for you. Choose your own password to keep using DMRHub.';

describe('a user with a temporary password', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
  });

  afterEach(() => {
    vi.doUnmock('@/services/API');
    vi.unstubAllGlobals();
  });

  async function settingsAPI() {
    vi.doMock('@/services/API', () => ({
      default: { get: vi.fn(), put: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
    }));
    const { default: API } = await import('@/services/API');
    return API as unknown as { get: Mock; patch: Mock };
  }

  it('sees only the Password card with the banner on Settings', async () => {
    const api = await settingsAPI();
    let must = true;
    api.get.mockImplementation((url: string) => {
      if (url === '/users/me') {
        return Promise.resolve({ data: { id: 1000001, callsign: 'N0CALL', must_change_password: must } });
      }
      if (must) {
        return Promise.reject(mustChangeError);
      }
      if (url === '/users/me/settings') {
        return Promise.resolve({ data: { sms_storage: false } });
      }
      if (url === '/users/me/radio-ids') {
        return Promise.resolve({ data: { radio_ids: [] } });
      }
      return Promise.resolve({ data: [] });
    });
    api.patch.mockImplementation(() => {
      must = false;
      return Promise.resolve({ data: { message: 'User updated' } });
    });
    const { default: SettingsPage } = await import('@/views/users/SettingsPage.vue');
    const { useUserStore } = await import('@/store');
    const wrapper = mount(SettingsPage, { attachTo: document.body });
    await flushPromises();

    expect(wrapper.find('[data-test="password-must-change"]').text()).toBe(banner);
    expect(wrapper.find('[data-test="password-card"]').exists()).toBe(true);
    for (const card of ['sms-storage-card', 'radio-ids-card', 'apps-card']) {
      expect(wrapper.find(`[data-test="${card}"]`).exists()).toBe(false);
    }
    expect(api.get.mock.calls.map((c) => c[0])).toEqual(['/users/me']);
    expect(useUserStore().mustChangePassword).toBe(true);

    await wrapper.find('#pw-current').setValue('Qm4tVx8rKp2Hw9Ns');
    await wrapper.find('#pw-new').setValue('my-own-password');
    await wrapper.find('#pw-confirm').setValue('my-own-password');
    await wrapper.find('form').trigger('submit');
    await flushPromises();

    expect(api.patch).toHaveBeenCalledWith('/users/1000001', {
      password: 'my-own-password',
      current_password: 'Qm4tVx8rKp2Hw9Ns',
    });
    expect(wrapper.find('[data-test="password-must-change"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="password-ok"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="sms-storage-card"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="radio-ids-card"]').exists()).toBe(true);
    expect(useUserStore().mustChangePassword).toBe(false);
  });

  it('is sent to Settings by the router until they choose a password', async () => {
    const { ownPasswordGuard } = await import('@/router');
    const { useUserStore } = await import('@/store');
    const to = (path: string) => ({ path }) as RouteLocationNormalized;
    const user = useUserStore();

    user.loggedIn = true;
    user.mustChangePassword = true;
    expect(ownPasswordGuard(to('/messages'))).toEqual({ path: '/settings' });
    expect(ownPasswordGuard(to('/admin/users'))).toEqual({ path: '/settings' });
    for (const path of ['/settings', '/login', '/forgot-password', '/reset-password']) {
      expect(ownPasswordGuard(to(path))).toBe(true);
    }

    user.mustChangePassword = false;
    expect(ownPasswordGuard(to('/messages'))).toBe(true);
    user.loggedIn = false;
    user.mustChangePassword = true;
    expect(ownPasswordGuard(to('/messages'))).toBe(true);
  });

  it('is sent to Settings when the API answers that the password must change', async () => {
    const location = { pathname: '/repeaters' };
    vi.stubGlobal('location', location);
    const { default: API } = await import('@/services/API');
    const handlers = (API.interceptors.response as unknown as {
      handlers: { rejected: (e: unknown) => Promise<unknown> }[];
    }).handlers;
    const rejected = handlers[0]!.rejected;

    await expect(rejected(mustChangeError)).rejects.toBe(mustChangeError);
    expect(location.pathname).toBe('/settings');

    location.pathname = '/messages';
    await expect(rejected(mustChangeError)).rejects.toBe(mustChangeError);
    expect(location.pathname, 'not to the login page as for other 403 answers').toBe('/settings');
  });

  it('is sent to Settings after logging in', async () => {
    const api = await settingsAPI();
    api.get.mockResolvedValue({ data: { id: 1000001, callsign: 'N0CALL', must_change_password: true } });
    (api as unknown as { post: Mock }).post.mockResolvedValue({ data: { message: 'Logged in' } });
    const { default: LoginPage } = await import('@/views/auth/LoginPage.vue');
    const { useUserStore } = await import('@/store');
    const push = vi.fn();
    const wrapper = mount(LoginPage, {
      global: {
        mocks: { $router: { push }, $route: { query: { next: '/messages' } }, $toast: { add: vi.fn() } },
        stubs: { RouterLink: { template: '<a><slot /></a>' } },
      },
    });
    await wrapper.find('#username').setValue('n0call');
    await wrapper.find('#password').setValue('Qm4tVx8rKp2Hw9Ns');
    await wrapper.find('form').trigger('submit');
    await flushPromises();

    expect(push).toHaveBeenCalledWith('/settings');
    expect(useUserStore().mustChangePassword).toBe(true);
  });
});
