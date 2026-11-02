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
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import PluginSwitchesCard from '@/components/users/PluginSwitchesCard.vue';
import { codeParts } from '@/services/pluginSwitches';

const mockAPI = API as unknown as { get: Mock; put: Mock };

function aprs(on: boolean, ssid = '7') {
  return {
    plugin_id: 3,
    plugin: 'APRS',
    key: 'aprs',
    title: 'APRS messages',
    description: 'Send and get APRS text messages with your radio.',
    notice: 'APRS messages go out on the internet, where anyone can read them.',
    details: [
      { label: 'Your APRS address', value: `\`N1CALL-${ssid}\`` },
      { label: 'To send', value: 'Text `4700` with the address, a space and the message, such as `N2CALL-9 Hello there`.' },
    ],
    choice: { label: 'SSID', options: ['5', '7', '9'], value: ssid },
    on,
  };
}

async function mountCard() {
  const wrapper = mount(PluginSwitchesCard, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('PluginSwitchesCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('shows nothing when no plugin has a switch, or the hub has no such list', async () => {
    mockAPI.get.mockResolvedValue({ data: [] });
    expect((await mountCard()).find('[data-test^="plugin-switch-"]').exists()).toBe(false);
    mockAPI.get.mockRejectedValue({ response: { status: 404 } });
    expect((await mountCard()).find('[data-test^="plugin-switch-"]').exists()).toBe(false);
  });

  it('shows a switch that is off with no details', async () => {
    mockAPI.get.mockResolvedValue({ data: [aprs(false)] });
    const wrapper = await mountCard();
    const card = wrapper.find('[data-test="plugin-switch-3-aprs"]');
    expect(card.text()).toContain('APRS messages');
    expect(card.text()).toContain('Send and get APRS text messages with your radio.');
    expect(card.find('[data-test="plugin-switch-plugin"]').text()).toBe('From the plugin APRS.');
    expect(card.find('[data-test="plugin-switch-status"]').text()).toBe('Off.');
    expect(card.find('[data-test="plugin-switch-details"]').exists()).toBe(false);
    expect(card.find('[role="switch"]').attributes('aria-checked')).toBe('false');
  });

  it('turns a switch on and shows the details, the choice and the notice', async () => {
    mockAPI.get.mockResolvedValue({ data: [aprs(false)] });
    mockAPI.put.mockResolvedValue({ data: aprs(true) });
    const wrapper = await mountCard();
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/plugin-switches/3/aprs', { on: true, choice: '7' });
    const card = wrapper.find('[data-test="plugin-switch-3-aprs"]');
    expect(card.find('[data-test="plugin-switch-status"]').text()).toBe('On.');
    const list = card.find('[data-test="plugin-switch-detail-list"]');
    expect(list.findAll('dt').map((d) => d.text())).toEqual(['Your APRS address', 'To send']);
    expect(list.findAll('.font-mono').map((m) => m.text())).toEqual(['N1CALL-7', '4700', 'N2CALL-9 Hello there']);
    expect(card.find('[data-test="plugin-switch-notice"]').text()).toBe(
      'APRS messages go out on the internet, where anyone can read them.',
    );
    expect((card.find('[data-test="plugin-switch-choice"]').element as HTMLSelectElement).value).toBe('7');
  });

  it('saves a new choice', async () => {
    mockAPI.get.mockResolvedValue({ data: [aprs(true)] });
    mockAPI.put.mockResolvedValue({ data: aprs(true, '9') });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="plugin-switch-choice"]').setValue('9');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/users/me/plugin-switches/3/aprs', { on: true, choice: '9' });
    expect(wrapper.find('[data-test="plugin-switch-detail-list"] .font-mono').text()).toBe('N1CALL-9');
  });

  it('says why a switch did not change', async () => {
    mockAPI.get.mockResolvedValue({ data: [aprs(true)] });
    mockAPI.put.mockRejectedValue({ response: { status: 404, data: { error: 'No such switch or option' } } });
    const wrapper = await mountCard();
    await wrapper.find('[role="switch"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="plugin-switch-error"]').text()).toBe('No such switch or option');
    expect(wrapper.find('[data-test="plugin-switch-status"]').text()).toBe('On.');
  });

  it('splits a detail into text and code', () => {
    expect(codeParts('Text `4700` now')).toEqual([
      { text: 'Text ', code: false },
      { text: '4700', code: true },
      { text: ' now', code: false },
    ]);
  });
});
