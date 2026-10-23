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
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import PositionsSettings from '@/components/setup/DMR/PositionsSettings.vue';
import SetupWizard from '@/views/SetupWizard.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const mountWizard = async (secrets: Record<string, boolean> = {}) => {
  mockAPI.get.mockResolvedValue({
    data: {
      dmr: { mmdvm: { port: 62031 }, positions: { 'min-interval': 10, 'max-retention': 0, 'aprs-is': { callsign: 'N0HUB-10', server: '' } } },
      http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
      secrets: { secretSet: true, passwordSaltSet: true, smtpPasswordSet: false, aprsPasscodeSet: false, ...secrets },
    },
  });
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  mockAPI.put.mockResolvedValue({ data: {} });
  const wrapper = mount(SetupWizard, {
    attachTo: document.body,
    global: {
      mocks: { $route: { path: '/admin/setup', query: {} }, $router: { push: vi.fn() }, $toast: { add: vi.fn() } },
    },
  });
  await flushPromises();
  return wrapper;
};

describe('Positions settings', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('shows plain labels, the defaults, and no config keys', () => {
    const wrapper = mount(PositionsSettings, { props: { modelValue: {} } });
    const text = wrapper.text();
    expect(text).toContain('Positions');
    expect(text).toContain('Shortest report interval');
    expect(text).toContain('Longest time to keep positions');
    expect(text).toContain('APRS-IS');
    expect(text).toContain('Leave the callsign empty to send nothing to APRS-IS.');
    expect((wrapper.find('#positions-min-interval').element as HTMLInputElement).value).toBe('5');
    expect((wrapper.find('#positions-max-retention').element as HTMLInputElement).value).toBe('30');
    expect((wrapper.find('#positions-aprs-passcode').element as HTMLInputElement).type).toBe('password');
    expect(text).not.toMatch(/min-interval|max-retention|aprs-is\.|dmr\./);
  });

  it('emits numbers, and the callsign in capitals', async () => {
    const wrapper = mount(PositionsSettings, { props: { modelValue: { 'aprs-is': { server: 'x:1' } } } });
    await wrapper.find('#positions-min-interval').setValue('15');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ 'aprs-is': { server: 'x:1' }, 'min-interval': 15 }]);
    await wrapper.find('#positions-aprs-callsign').setValue('n0hub-10');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ 'aprs-is': { server: 'x:1', callsign: 'N0HUB-10' } }]);
  });

  it('shows the validation errors of its fields', () => {
    const wrapper = mount(PositionsSettings, {
      props: {
        modelValue: {},
        errors: { 'min-interval': 'bad interval', 'aprs-is': { callsign: 'bad callsign', server: 'bad server' } },
      },
    });
    expect(wrapper.text()).toContain('bad interval');
    expect(wrapper.text()).toContain('bad callsign');
    expect(wrapper.text()).toContain('bad server');
  });

  it('says whether a passcode is stored', () => {
    const stored = mount(PositionsSettings, { props: { modelValue: {}, passcodeSet: true } });
    expect(stored.find('[data-test="aprs-passcode-status"]').text()).toBe('Stored. Leave blank to keep it.');
    const none = mount(PositionsSettings, { props: { modelValue: {} } });
    expect(none.find('[data-test="aprs-passcode-status"]').text()).toBe('Not set.');
  });

  it('is saved with the DMR settings, with the passcode sent apart', async () => {
    const wrapper = await mountWizard({ aprsPasscodeSet: true });
    const card = wrapper.findComponent(PositionsSettings);
    expect(card.exists()).toBe(true);
    expect(card.props('passcodeSet')).toBe(true);
    await card.find('#positions-aprs-passcode').setValue('14215');
    await flushPromises();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const payload = mockAPI.put.mock.calls[0]![1] as Record<string, unknown> & { dmr: { positions: Record<string, unknown> } };
    expect(payload['aprs-is-passcode']).toBe('14215');
    expect(payload.dmr.positions['aprs-is']).toEqual({ callsign: 'N0HUB-10', server: '' });
    expect(payload.dmr.positions['min-interval']).toBe(10);
  });

  it('keeps the stored passcode when the field is left blank', async () => {
    const wrapper = await mountWizard({ aprsPasscodeSet: true });
    const card = wrapper.findComponent(PositionsSettings);
    await card.find('#positions-aprs-passcode').setValue('');
    await flushPromises();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const payload = mockAPI.put.mock.calls[0]![1] as Record<string, unknown> & { dmr: { positions: Record<string, Record<string, unknown>> } };
    expect(payload).not.toHaveProperty('aprs-is-passcode');
    expect(payload.dmr.positions['aprs-is']).not.toHaveProperty('passcode');
  });
});
