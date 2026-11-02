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
  },
}));

import API from '@/services/API';
import StoreForwardCard from '@/components/users/StoreForwardCard.vue';

const mockAPI = API as unknown as { get: Mock; put: Mock };

const held = [
  {
    id: 1,
    message_id: 10,
    text: 'Net starts at 8, see you there',
    source_id: 3002069,
    source_callsign: 'N8CALL',
    radio_id: 3002068,
    held_at: '2026-10-31T21:14:00Z',
    format_known: true,
  },
  {
    id: 2,
    message_id: 11,
    text: 'Lunch Saturday?',
    source_id: 3004312,
    source_callsign: '',
    radio_id: 3002077,
    held_at: '2026-11-01T09:02:00Z',
    format_known: false,
  },
];

async function mountCard(enabled: boolean, holdDays = 7) {
  const wrapper = mount(StoreForwardCard, { props: { enabled, holdDays }, attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('StoreForwardCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    mockAPI.get.mockImplementation((url: string) => {
      if (url === '/users/me/held-texts') {
        return Promise.resolve({ data: { held } });
      }
      return Promise.reject(new Error(`unexpected GET ${url}`));
    });
  });

  it('shows store and forward off with no waiting messages', async () => {
    const wrapper = await mountCard(false);
    expect(wrapper.find('[data-test="store-forward-status"]').text()).toBe('Off.');
    expect(wrapper.find('[role="switch"]').attributes('aria-checked')).toBe('false');
    expect(wrapper.find('[data-test="store-forward-held"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="store-forward-on"]').exists()).toBe(false);
    expect(mockAPI.get).not.toHaveBeenCalled();
  });

  it('lists the messages waiting for my radios and says which wait for the radio format', async () => {
    const wrapper = await mountCard(true, 7);
    expect(wrapper.find('[data-test="store-forward-status"]').text()).toBe('On.');
    expect(wrapper.find('[data-test="store-forward-on"]').text()).toBe(
      'Store and forward is on. Messages wait up to 7 days.',
    );
    const first = wrapper.find('[data-test="held-1"]');
    expect(first.text()).toContain('"Net starts at 8, see you there"');
    expect(first.text()).toContain('N8CALL');
    expect(first.text()).toContain('3002068');
    expect(first.find('[data-test="held-format-unknown"]').exists()).toBe(false);
    const second = wrapper.find('[data-test="held-2"]');
    expect(second.text()).toContain('3004312');
    expect(second.find('[data-test="held-format-unknown"]').text()).toBe(
      "Waits until your radio sends a text, so DMRHub can send it in your radio's format.",
    );
  });

  it('turns store and forward on and loads the waiting messages', async () => {
    mockAPI.put.mockResolvedValue({ data: { sms_storage: false, store_forward: true, hold_days: 3 } });
    const wrapper = await mountCard(false);
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/store-forward', { enabled: true });
    expect(wrapper.find('[data-test="store-forward-on"]').text()).toBe(
      'Store and forward is on. Messages wait up to 3 days.',
    );
    expect(wrapper.findAll('[data-test^="held-"]').filter((r) => r.element.tagName === 'TR')).toHaveLength(2);
  });

  it('turns store and forward off and hides the waiting messages', async () => {
    mockAPI.put.mockResolvedValue({ data: { sms_storage: false, store_forward: false, hold_days: 7 } });
    const wrapper = await mountCard(true);
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/settings/store-forward', { enabled: false });
    expect(wrapper.find('[data-test="store-forward-status"]').text()).toBe('Off.');
    expect(wrapper.find('[data-test="store-forward-held"]').exists()).toBe(false);
  });

  it('says so when the switch cannot be saved', async () => {
    mockAPI.put.mockRejectedValue({ response: { status: 500, data: { error: 'Try again later' } } });
    const wrapper = await mountCard(false);
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(wrapper.text()).toContain('Try again later');
    expect(wrapper.find('[data-test="store-forward-status"]').text()).toBe('Off.');
  });
});
