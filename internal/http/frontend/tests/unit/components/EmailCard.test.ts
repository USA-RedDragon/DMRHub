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
  default: { get: vi.fn(), put: vi.fn() },
}));

import API from '@/services/API';
import EmailCard from '@/components/users/EmailCard.vue';

const mockAPI = API as unknown as { get: Mock; put: Mock };

async function card(smtp: boolean, current = '', verified = false) {
  const pinia = createPinia();
  setActivePinia(pinia);
  mockAPI.get.mockResolvedValue({ data: { password_reset: smtp } });
  const wrapper = mount(EmailCard, { props: { current, verified }, global: { plugins: [pinia] } });
  await flushPromises();
  return wrapper;
}

describe('EmailCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('says a reset needs an admin when the network cannot send email', async () => {
    const wrapper = await card(false);
    expect(wrapper.find('[data-test="email-off"]').text()).toBe('This network cannot send email. To reset a forgotten password, ask an admin.');
    expect(wrapper.find('input#email').exists()).toBe(false);
  });

  it('shows whether the address is confirmed', async () => {
    expect((await card(true, 'n0call@example.com', true)).find('[data-test="email-status"]').text()).toContain('Confirmed');
    expect((await card(true, 'n0call@example.com', false)).find('[data-test="email-status"]').text()).toContain('Not confirmed');
  });

  it('saves a new address as not confirmed', async () => {
    mockAPI.put.mockResolvedValue({ data: { message: 'Email saved. Open the link we sent to it to confirm it.' } });
    const wrapper = await card(true, 'old@example.com', true);
    await wrapper.find('input#email').setValue('new@example.com');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/email', { email: 'new@example.com' });
    expect(wrapper.find('[data-test="email-ok"]').text()).toBe('Email saved. Open the link we sent to it to confirm it.');
    expect(wrapper.find('[data-test="email-status"]').text()).toContain('new@example.com');
    expect(wrapper.find('[data-test="email-status"]').text()).toContain('Not confirmed');
  });

  it('shows the server error for a bad address', async () => {
    mockAPI.put.mockRejectedValue({ response: { data: { error: 'Enter an email address, such as n0call@example.com' } } });
    const wrapper = await card(true);
    await wrapper.find('input#email').setValue('nope');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="email-error"]').text()).toBe('Enter an email address, such as n0call@example.com');
  });
});
