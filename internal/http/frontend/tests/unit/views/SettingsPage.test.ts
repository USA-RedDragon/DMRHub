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
    put: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import SettingsPage from '@/views/users/SettingsPage.vue';

const mockAPI = API as unknown as { get: Mock; put: Mock; post: Mock; delete: Mock };

const accountID = { id: 3191868, callsign: 'KI5VMF', account: true, verified_at: null };
const addedID = { id: 3191877, callsign: 'KI5VMF', account: false, verified_at: '2026-10-10T20:41:37Z' };

const privacy =
  'When SMS Storage is on, the text messages sent to and from the radio IDs that you own appear on your ' +
  'Messages page, up to the 250 newest in each box.';

function serve(smsStorage: boolean, ids = [accountID, addedID]) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/users/me') {
      return Promise.resolve({ data: { id: 3191868, callsign: 'KI5VMF', must_change_password: false } });
    }
    if (url === '/users/me/settings') {
      return Promise.resolve({ data: { sms_storage: smsStorage } });
    }
    if (url === '/users/me/radio-ids') {
      return Promise.resolve({ data: { radio_ids: ids } });
    }
    if (url === '/oauth/apps') {
      return Promise.resolve({ data: [] });
    }
    return Promise.reject(new Error(`unexpected GET ${url}`));
  });
}

async function mountPage() {
  const wrapper = mount(SettingsPage, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('asks a visitor who is not logged in to log in', async () => {
    mockAPI.get.mockRejectedValue({ response: { status: 401, data: { error: 'Authentication failed' } } });
    const wrapper = await mountPage();
    expect(wrapper.text()).toContain('Log in to change your settings');
    expect(wrapper.find('[data-test="sms-storage-card"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="radio-ids-card"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="apps-card"]').exists()).toBe(false);
  });

  it('shows the Password card first, for the logged-in user', async () => {
    serve(false);
    const wrapper = await mountPage();
    const cards = wrapper.findAll('[data-test$="-card"]').map((c) => c.attributes('data-test'));
    expect(cards[0]).toBe('password-card');
    expect(wrapper.find('[data-test="password-card"]').text()).toContain('Change password');
  });

  it('shows the apps that can use the account', async () => {
    serve(false);
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="apps-card"]').text()).toContain('No apps can use your account.');
  });

  it('shows SMS Storage off with the privacy text and no dates or config keys', async () => {
    serve(false);
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="sms-status"]').text()).toBe('Off.');
    expect(wrapper.find('[data-test="sms-privacy"]').text()).toBe(privacy);
    expect(wrapper.find('[role="switch"]').attributes('aria-checked')).toBe('false');
    const text = wrapper.text();
    expect(text).not.toMatch(/dmr\./);
    expect(text).not.toMatch(/since|older|before|records all/i);
  });

  it('turns SMS Storage on without a confirmation', async () => {
    serve(false);
    mockAPI.put.mockResolvedValue({ data: { sms_storage: true } });
    const wrapper = await mountPage();
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/sms-storage', { enabled: true });
    expect(wrapper.find('[data-test="sms-status"]').text()).toBe('On.');
    expect(wrapper.text()).toContain('SMS Storage is on.');
  });

  it('asks before it turns SMS Storage off', async () => {
    serve(true);
    mockAPI.put.mockResolvedValue({ data: { sms_storage: false } });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="sms-status"]').text()).toBe('On.');
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).not.toHaveBeenCalled();
    const confirm = wrapper.find('[data-test="sms-confirm-off"]');
    expect(confirm.text()).toContain('Your inbox and outbox will be empty.');

    const cancel = confirm.findAll('button').find((b) => b.text() === 'Cancel');
    await cancel!.trigger('click');
    expect(wrapper.find('[data-test="sms-confirm-off"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="sms-status"]').text()).toBe('On.');

    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    const off = wrapper.find('[data-test="sms-confirm-off"]').findAll('button').find((b) => b.text() === 'Turn off');
    await off!.trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/sms-storage', { enabled: false });
    expect(wrapper.find('[data-test="sms-status"]').text()).toBe('Off.');
  });

  it('lists the radio IDs and keeps the account ID', async () => {
    serve(true);
    const wrapper = await mountPage();
    const account = wrapper.find('[data-test="radio-id-3191868"]');
    expect(account.text()).toContain('Account ID');
    expect(account.text()).toContain('Cannot remove');
    expect(account.find('button').exists()).toBe(false);
    const added = wrapper.find('[data-test="radio-id-3191877"]');
    expect(added.text()).toContain('Verified');
    expect(added.find('button').text()).toBe('Remove');
  });

  it('checks a new radio ID before it sends it', async () => {
    serve(true);
    const wrapper = await mountPage();
    await wrapper.find('#add-radio-id').setValue('314059801');
    await wrapper.find('[data-test="add-radio-id"]').trigger('submit');
    expect(wrapper.find('[data-test="radio-id-error"]').text()).toContain('Enter a 7-digit DMR ID');
    await wrapper.find('#add-radio-id').setValue('3191877');
    await wrapper.find('[data-test="add-radio-id"]').trigger('submit');
    expect(wrapper.find('[data-test="radio-id-error"]').text()).toBe('3191877 is already on your list.');
    expect(mockAPI.post).not.toHaveBeenCalled();
  });

  it('adds a radio ID and shows the server error when RadioID.net disagrees', async () => {
    serve(true, [accountID]);
    mockAPI.post.mockResolvedValueOnce({ data: addedID });
    const wrapper = await mountPage();
    await wrapper.find('#add-radio-id').setValue('3191877');
    await wrapper.find('[data-test="add-radio-id"]').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/users/me/radio-ids', { id: 3191877 });
    expect(wrapper.find('[data-test="radio-id-3191877"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="radio-id-ok"]').text()).toBe('Added 3191877.');

    mockAPI.post.mockRejectedValueOnce({
      response: { status: 400, data: { error: 'RadioID.net does not list 3140598 under your callsign' } },
    });
    await wrapper.find('#add-radio-id').setValue('3140598');
    await wrapper.find('[data-test="add-radio-id"]').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="radio-id-error"]').text()).toBe(
      'RadioID.net does not list 3140598 under your callsign',
    );
  });

  it('removes a radio ID', async () => {
    serve(true);
    mockAPI.delete.mockResolvedValue({ data: { message: 'Radio ID removed' } });
    const wrapper = await mountPage();
    await wrapper.find('[data-test="radio-id-3191877"] button').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/users/me/radio-ids/3191877');
    expect(wrapper.find('[data-test="radio-id-3191877"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="radio-id-ok"]').text()).toBe('Removed 3191877.');
  });
});
