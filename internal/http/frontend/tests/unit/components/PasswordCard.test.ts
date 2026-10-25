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
    patch: vi.fn(),
  },
}));

import API from '@/services/API';
import PasswordCard from '@/components/users/PasswordCard.vue';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

async function fill(wrapper: ReturnType<typeof mount>, current: string, next: string, confirm: string) {
  await wrapper.find('#pw-current').setValue(current);
  await wrapper.find('#pw-new').setValue(next);
  await wrapper.find('#pw-confirm').setValue(confirm);
  await wrapper.find('form').trigger('submit');
  await flushPromises();
}

describe('PasswordCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the three fields and the button of the mockup', () => {
    const wrapper = mount(PasswordCard, { props: { userId: 3191868 } });
    expect(wrapper.find('[data-test="password-card"]').text()).toContain('Password');
    expect(wrapper.find('label[for="pw-current"]').text()).toBe('Current password');
    expect(wrapper.find('label[for="pw-new"]').text()).toBe('New password');
    expect(wrapper.find('label[for="pw-confirm"]').text()).toBe('Confirm new password');
    expect(wrapper.find('#pw-current').attributes('autocomplete')).toBe('current-password');
    expect(wrapper.find('#pw-new').attributes('type')).toBe('password');
    expect(wrapper.find('button[type="submit"]').text()).toBe('Change password');
  });

  it.each([
    ['', 'new', 'new', 'Enter your current password.'],
    ['old', '', '', 'Enter a new password.'],
    ['old', 'new', 'other', 'The new passwords do not match.'],
  ])('checks the fields before it sends them (%s, %s, %s)', async (current, next, confirm, want) => {
    const wrapper = mount(PasswordCard, { props: { userId: 3191868 } });
    await fill(wrapper, current, next, confirm);
    expect(wrapper.find('[data-test="password-error"]').text()).toBe(want);
    expect(mockAPI.patch).not.toHaveBeenCalled();
  });

  it('sends the current and the new password and says the other sessions ended', async () => {
    mockAPI.patch.mockResolvedValue({ data: { message: 'User updated' } });
    const wrapper = mount(PasswordCard, { props: { userId: 3191868 } });
    await fill(wrapper, 'old-password', 'new-password', 'new-password');
    expect(mockAPI.patch).toHaveBeenCalledWith('/users/3191868', {
      password: 'new-password',
      current_password: 'old-password',
    });
    expect(wrapper.find('[data-test="password-ok"]').text()).toBe(
      'Your password is changed. Your other sessions are logged out.',
    );
    expect(wrapper.find('[data-test="password-error"]').exists()).toBe(false);
    expect((wrapper.find('#pw-new').element as HTMLInputElement).value).toBe('');
    expect(wrapper.emitted('changed')).toHaveLength(1);
  });

  it.each([
    'The current password is wrong.',
    'Password has been reported in a data breach. Please use another one',
  ])('shows the error of the server: %s', async (message) => {
    mockAPI.patch.mockRejectedValue({ response: { status: 400, data: { error: message } } });
    const wrapper = mount(PasswordCard, { props: { userId: 3191868 } });
    await fill(wrapper, 'old-password', 'new-password', 'new-password');
    expect(wrapper.find('[data-test="password-error"]').text()).toBe(message);
    expect(wrapper.find('[data-test="password-ok"]').exists()).toBe(false);
    expect(wrapper.emitted('changed')).toBeUndefined();
  });
});
