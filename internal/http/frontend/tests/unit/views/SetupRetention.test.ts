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
import { Select } from '@/components/ui/select';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import RetentionSettings from '@/components/setup/DMR/RetentionSettings.vue';
import { retentionChoices, retentionDays } from '@/lib/retention';
import SetupWizard from '@/views/SetupWizard.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

describe('Retention settings', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('shows plain labels, keep forever by default, and no config keys', () => {
    const wrapper = mount(RetentionSettings, { props: { modelValue: {} } });
    const text = wrapper.text();
    expect(text).toContain('Retention');
    expect(text).toContain('How long DMRHub keeps records.');
    expect(text).toContain('Messages');
    expect(text).toContain('Transmissions');
    expect(text).toContain('Raw bursts');
    expect(wrapper.findAllComponents(Select)[0]!.props('modelValue')).toBe('0');
    expect(text).not.toMatch(/dmr\.|retention\.|\(0\)/);
  });

  it('offers the choices, plus a value from the config file that is not a choice as days', () => {
    const wrapper = mount(RetentionSettings, { props: { modelValue: { messages: 45, transmissions: 365, bursts: 1 } } });
    expect(wrapper.findAllComponents(Select).map((x) => x.props('modelValue'))).toEqual(['45', '365', '1']);
    expect(retentionChoices(45).map((c) => c.label)).toEqual([
      'Keep forever', '30 days', '45 days', '90 days', '1 year', '2 years', '5 years',
    ]);
    expect(retentionChoices(365)).toHaveLength(6);
    expect(retentionChoices(1).map((c) => c.label)).toContain('1 day');
    expect(retentionDays(undefined)).toBe(0);
    expect(retentionDays(-5)).toBe(0);
    expect(retentionDays('90')).toBe(90);
  });

  it('emits the number of days', async () => {
    const wrapper = mount(RetentionSettings, { props: { modelValue: { messages: 0, transmissions: 0, bursts: 0 } } });
    wrapper.findAllComponents(Select)[1]!.vm.$emit('update:modelValue', '730');
    await flushPromises();
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ messages: 0, transmissions: 730, bursts: 0 }]);
  });

  it('shows the validation error of a field', () => {
    const wrapper = mount(RetentionSettings, {
      props: {
        modelValue: { messages: 99999 },
        errors: { messages: 'retention must be 0 (keep forever) or a number of days up to 36500' },
      },
    });
    expect(wrapper.text()).toContain('retention must be 0 (keep forever) or a number of days up to 36500');
  });

  it('is saved with the rest of the DMR settings on the Setup page', async () => {
    mockAPI.get.mockResolvedValue({
      data: {
        dmr: { 'radio-id-url': 'https://radioid.net/static/users.json', mmdvm: { port: 62031 }, retention: { messages: 0, transmissions: 0, bursts: 0 } },
        http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
      },
    });
    mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
    mockAPI.put.mockResolvedValue({ data: {} });
    const toast = { add: vi.fn() };
    const wrapper = mount(SetupWizard, {
      attachTo: document.body,
      global: {
        mocks: { $route: { path: '/admin/setup', query: {} }, $router: { push: vi.fn() }, $toast: toast },
      },
    });
    await flushPromises();
    const dmr = wrapper.findComponent(RetentionSettings);
    expect(dmr.exists()).toBe(true);
    dmr.vm.$emit('update:modelValue', { messages: 365, transmissions: 90, bursts: 30 });
    await flushPromises();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const payload = mockAPI.put.mock.calls[0]![1] as { dmr: Record<string, unknown> };
    expect(payload.dmr.retention).toEqual({ messages: 365, transmissions: 90, bursts: 30 });
    expect((payload.dmr.mmdvm as { port: number }).port).toBe(62031);
  });
});
