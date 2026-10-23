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

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

const route = vi.hoisted(() => ({ params: { id: '3191868' } }));

vi.mock('vue-router', () => ({
  useRoute: () => route,
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
}));

import API from '@/services/API';
import RadioPositionsPage from '@/views/positions/RadioPositionsPage.vue';

const mockAPI = API as unknown as { get: Mock; put: Mock };

const setting = {
  radio_id: 3191868,
  tracking: true,
  interval_minutes: 10,
  retention_hours: 24,
  aprs: true,
  aprs_ssid: '9',
  aprs_symbol: '',
  aprs_comment: 'DMR 3191868',
};

function serve(aprs = true) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/positions/radios') {
      return Promise.resolve({
        data: { radios: [setting], intervals: [5, 10, 15, 30, 60], retention_hours: [24, 168, 720], aprs, callsign: 'KI5VMF' },
      });
    }
    if (url === '/positions/3191868') {
      const latest = { radio_id: 3191868, received_at: new Date(Date.now() - 4 * 60000).toISOString(), service_id: 5059 };
      return Promise.resolve({ data: { latest, track: [latest] } });
    }
    return Promise.reject({ response: { status: 404 } });
  });
}

async function mountPage() {
  const wrapper = mount(RadioPositionsPage, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('RadioPositionsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    route.params.id = '3191868';
  });

  it('shows the setting of the radio as the mockup does', async () => {
    serve();
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="radio-subtitle"]').text()).toBe('KI5VMF · last report 4 min ago');
    expect((wrapper.find('#interval').element as HTMLSelectElement).value).toBe('10');
    expect(wrapper.findAll('#keep option').map((o) => o.text())).toEqual(['24 hours', '7 days', '30 days']);
    expect(wrapper.find('[data-test="aprs-status"]').text()).toBe('On, as KI5VMF-9.');
    expect(wrapper.find('#symbol option').text()).toBe('As the radio reports it (car)');
    expect(wrapper.findAll('#ssid option').map((o) => o.text())).toEqual(['-7 handheld', '-9 mobile', '-5 other network', '-10 internet', 'none']);
    expect(wrapper.find('[data-test="download-gpx"]').attributes('href')).toBe('/api/v1/positions/3191868/track?format=gpx');
    expect(wrapper.text()).not.toMatch(/interval_minutes|aprs_ssid|min-interval/);
  });

  it('saves the whole setting', async () => {
    serve();
    mockAPI.put.mockImplementation((_url: string, body: Record<string, unknown>) => Promise.resolve({ data: { radio_id: 3191868, ...body } }));
    const wrapper = await mountPage();
    await wrapper.find('#interval').setValue('30');
    await wrapper.find('#symbol').setValue('bike');
    await wrapper.find('#ssid').setValue('7');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/positions/3191868/settings', {
      tracking: true,
      interval_minutes: 30,
      retention_hours: 24,
      aprs: true,
      aprs_ssid: '7',
      aprs_symbol: 'bike',
      aprs_comment: 'DMR 3191868',
    });
    expect(wrapper.find('[data-test="save-message"]').text()).toBe('Saved.');
    expect(wrapper.find('[data-test="aprs-status"]').text()).toBe('On, as KI5VMF-7.');
  });

  it('turns APRS-IS off with tracking', async () => {
    serve();
    mockAPI.put.mockImplementation((_url: string, body: Record<string, unknown>) => Promise.resolve({ data: { radio_id: 3191868, ...body } }));
    const wrapper = await mountPage();
    wrapper.findAllComponents({ name: 'Switch' })[0]!.vm.$emit('update:modelValue', false);
    await flushPromises();
    expect(wrapper.find('[data-test="aprs-status"]').text()).toBe('Off.');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put.mock.calls[0]![1]).toMatchObject({ tracking: false, aprs: false });
  });

  it('shows what the hub refuses', async () => {
    serve();
    mockAPI.put.mockRejectedValue({ response: { status: 400, data: { error: 'Pick a report interval that this hub allows' } } });
    const wrapper = await mountPage();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="save-message"]').text()).toBe('Pick a report interval that this hub allows');
  });

  it('says when the hub does not send to APRS-IS', async () => {
    serve(false);
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="aprs-unavailable"]').exists()).toBe(true);
    expect((wrapper.find('#symbol').element as HTMLSelectElement).disabled).toBe(true);
  });

  it('refuses a radio the user does not own', async () => {
    serve();
    route.params.id = '3140598';
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="radio-missing"]').exists()).toBe(true);
    expect(wrapper.find('form').exists()).toBe(false);
  });
});
