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
import OpenBridgeSettings from '@/components/setup/DMR/OpenBridgeSettings.vue';
import SetupWizard from '@/views/SetupWizard.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const config = (openbridge: Record<string, unknown>) => ({
  data: {
    dmr: { 'radio-id-url': 'https://radioid.net/static/users.json', mmdvm: { port: 62031 }, openbridge },
    http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
  },
});

const mountWizard = (path: string) => mount(SetupWizard, {
  attachTo: document.body,
  global: {
    mocks: { $route: { path, query: {} }, $router: { push: vi.fn() }, $toast: { add: vi.fn() } },
  },
});

describe('OpenBridge settings', { timeout: 20000 }, () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('shows plain labels and no config keys', () => {
    const wrapper = mount(OpenBridgeSettings, { props: { modelValue: { enabled: true } } });
    const text = wrapper.text();
    expect(text).toContain('OpenBridge Settings');
    expect(text).toContain('Server ID');
    expect(text).not.toMatch(/dmr\.|openbridge\.|server-id/);
  });

  it('uses ids that no other DMR card uses', () => {
    const wrapper = mount(OpenBridgeSettings, { props: { modelValue: { enabled: true } } });
    const ids = wrapper.findAll('[id]').map((e) => e.attributes('id'));
    expect(ids).toEqual(['openbridge-enabled', 'openbridge-bind', 'openbridge-port', 'openbridge-server-id']);
    for (const label of wrapper.findAll('label')) {
      expect(ids).toContain(label.attributes('for'));
    }
  });

  it('emits numbers for the port and the server ID', async () => {
    const wrapper = mount(OpenBridgeSettings, { props: { modelValue: { enabled: true, port: 62035 } } });
    await wrapper.find('#openbridge-server-id').setValue('3129');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ enabled: true, port: 62035, 'server-id': 3129 }]);
    await wrapper.find('#openbridge-port').setValue('');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ enabled: true, port: 0 }]);
  });

  it('shows the validation error of the server ID', () => {
    const wrapper = mount(OpenBridgeSettings, {
      props: {
        modelValue: { enabled: true, 'server-id': 12 },
        errors: { 'server-id': 'the OpenBridge server ID must be 0 or 4 to 7 digits' },
      },
    });
    expect(wrapper.text()).toContain('the OpenBridge server ID must be 0 or 4 to 7 digits');
  });

  for (const path of ['/setup', '/admin/setup']) {
    it(`is shown on ${path} and saves the server ID`, async () => {
      mockAPI.get.mockResolvedValue(config({ enabled: true, bind: '[::]', port: 62035 }));
      mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
      mockAPI.put.mockResolvedValue({ data: {} });
      const wrapper = mountWizard(path);
      await flushPromises();
      const input = wrapper.find('#openbridge-server-id');
      expect(input.exists()).toBe(true);
      expect(wrapper.find('label[for="openbridge-server-id"]').text()).toBe('Server ID');
      await input.setValue('3129');
      await wrapper.find('form').trigger('submit');
      await flushPromises();
      const payload = mockAPI.put.mock.calls[0]![1] as { dmr: { openbridge: Record<string, unknown> } };
      expect(payload.dmr.openbridge).toEqual({ enabled: true, bind: '[::]', port: 62035, 'server-id': 3129 });
    });
  }

  it('shows the server ID error from the server on the Setup page', async () => {
    mockAPI.get.mockResolvedValue(config({ enabled: true, bind: '[::]', port: 62035, 'server-id': 12 }));
    mockAPI.post.mockResolvedValue({
      data: { valid: false, errors: { dmr: { openbridge: { 'server-id': 'the OpenBridge server ID must be 0 or 4 to 7 digits' } } } },
    });
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('the OpenBridge server ID must be 0 or 4 to 7 digits');
  });
});
