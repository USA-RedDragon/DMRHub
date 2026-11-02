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

import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
import OnAirCodeCard from '@/components/users/OnAirCodeCard.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; delete: Mock };

const unlock = { radio: 1000101, repeater: 100101, callsign: 'N0CODE', slot: 2, until: '2026-11-02T09:41:00Z' };
const off = { on: false, unlock_seconds: 86400, unlocks: [] };
const on = { on: true, unlock_seconds: 86400, unlocks: [unlock] };
const setup = { key: 'JBSWY3DPEHPK3PXPMFRGGZDF', uri: 'otpauth://totp/DMRHub:N0CODE?secret=JBSWY3DPEHPK3PXPMFRGGZDF', qr: 'data:image/png;base64,AAAA' };

describe('OnAirCodeCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sets up with a QR code and the key, and turns on only with 6 digits', async () => {
    mockAPI.get.mockResolvedValue({ data: off });
    mockAPI.post.mockImplementation((url: string) =>
      Promise.resolve({ data: url === '/users/me/totp/setup' ? setup : on }),
    );
    const wrapper = mount(OnAirCodeCard);
    await flushPromises();
    expect(wrapper.find('[data-test="on-air-steps"]').exists()).toBe(false);
    await wrapper.find('[data-test="on-air-setup"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/users/me/totp/setup');
    expect(wrapper.find('[data-test="on-air-qr"]').attributes('src')).toBe(setup.qr);
    expect(wrapper.find('[data-test="on-air-key"]').text()).toBe('JBSW Y3DP EHPK 3PXP MFRG GZDF');

    await wrapper.find('#on-air-code').setValue('12345');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="on-air-error"]').text()).toBe('Enter the 6-digit code from your app.');
    expect(mockAPI.post).toHaveBeenCalledTimes(1);

    await wrapper.find('#on-air-code').setValue('482913');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenLastCalledWith('/users/me/totp', { code: '482913' });
    expect(wrapper.find('[data-test="on-air-qr"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('JBSW');
    expect(wrapper.find('[data-test="on-air-on"]').text()).toContain('private call to 9 and the 6-digit code');
    expect(wrapper.find('[data-test="on-air-on"]').text()).toContain('for 24 hours');
    expect(wrapper.find('[data-test="on-air-note"]').text()).toBe('The on-air code is on.');
  });

  it('shows the error of a wrong code and stays in the setup', async () => {
    mockAPI.get.mockResolvedValue({ data: off });
    mockAPI.post.mockImplementation((url: string) =>
      url === '/users/me/totp/setup'
        ? Promise.resolve({ data: setup })
        : Promise.reject({ response: { status: 400, data: { error: 'Wrong code. Enter the code that your app shows now' } } }),
    );
    const wrapper = mount(OnAirCodeCard);
    await flushPromises();
    await wrapper.find('[data-test="on-air-setup"]').trigger('click');
    await flushPromises();
    await wrapper.find('#on-air-code').setValue('000000');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="on-air-error"]').text()).toBe('Wrong code. Enter the code that your app shows now');
    expect(wrapper.find('[data-test="on-air-qr"]').exists()).toBe(true);
  });

  it('lists the unlocks and locks one', async () => {
    mockAPI.get.mockResolvedValue({ data: on });
    mockAPI.delete.mockResolvedValue({ data: { ...on, unlocks: [] } });
    const wrapper = mount(OnAirCodeCard);
    await flushPromises();
    const row = wrapper.find('[data-test="unlock-1000101-100101-2"]');
    expect(row.text()).toContain('1000101');
    expect(row.text()).toContain('N0CODE 100101, TS2');
    await row.find('[data-test="lock"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/users/me/totp/unlocks/1000101/100101/2');
    expect(wrapper.find('[data-test="on-air-none"]').text()).toBe('No radio is unlocked now.');
  });

  it('turns off only after a code and a second step', async () => {
    mockAPI.get.mockResolvedValue({ data: on });
    mockAPI.delete.mockResolvedValue({ data: off });
    const wrapper = mount(OnAirCodeCard);
    await flushPromises();
    await wrapper.find('[data-test="on-air-off"]').trigger('click');
    const confirm = wrapper.find('[data-test="on-air-confirm-off"]');
    expect(confirm.text()).toContain('Anyone can then transmit with your radio IDs.');
    expect(mockAPI.delete).not.toHaveBeenCalled();
    await confirm.find('input').setValue('123 456');
    await confirm.trigger('submit');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/users/me/totp', { data: { code: '123456' } });
    expect(wrapper.find('[data-test="on-air-setup"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="on-air-note"]').text()).toBe('The on-air code is off.');
  });

  it('says the length of an unlock that Setup chose', async () => {
    mockAPI.get.mockResolvedValue({ data: { ...on, unlock_seconds: 900 } });
    const wrapper = mount(OnAirCodeCard);
    await flushPromises();
    expect(wrapper.find('[data-test="on-air-on"]').text()).toContain('for 15 minutes');
  });
});
