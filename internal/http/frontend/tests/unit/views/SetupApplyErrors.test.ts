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
import SetupWizard from '@/views/SetupWizard.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const mountSetup = async () => {
  mockAPI.get.mockResolvedValue({
    data: {
      dmr: { mmdvm: { bind: '[::]', port: 62031 }, openbridge: {}, ipsc: {}, retention: {} },
      http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
    },
  });
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  const toast = { add: vi.fn() };
  const wrapper = mount(SetupWizard, {
    attachTo: document.body,
    global: {
      mocks: { $route: { path: '/admin/setup', query: {} }, $router: { push: vi.fn() }, $toast: toast },
    },
  });
  await flushPromises();
  return { wrapper, toast };
};

const conflict = (applyErrors: unknown[]) => ({
  response: { status: 409, data: { error: 'Some settings could not be applied', applyErrors } },
});

describe('Setup page apply errors', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('names each server that could not take its new settings, in plain words', async () => {
    mockAPI.put.mockRejectedValue(conflict([
      { server: 'mmdvm', running: true, error: 'listen udp 127.0.0.1:62031: bind: address already in use' },
      { server: 'openbridge', running: false, error: 'listen udp [::]:62035: bind: address already in use' },
    ]));
    const { wrapper, toast } = await mountSetup();
    await wrapper.find('form').trigger('submit');
    await flushPromises();

    const alert = wrapper.find('[data-test="apply-errors"]');
    expect(alert.exists()).toBe(true);
    const text = alert.text();
    expect(text).toContain('The MMDVM server could not start with the new settings. It keeps running with its previous settings.');
    expect(text).toContain('listen udp 127.0.0.1:62031: bind: address already in use');
    expect(text).toContain('The OpenBridge server could not start with the new settings. It is not running.');
    expect(text).not.toMatch(/dmr\.|\bmmdvm\b|\bopenbridge\b|\btrue\b|\bfalse\b/);
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error' }));
  });

  it('clears the apply errors after a save that applies', async () => {
    mockAPI.put.mockRejectedValueOnce(conflict([{ server: 'ipsc', running: true, error: 'bind: address already in use' }]));
    const { wrapper } = await mountSetup();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="apply-errors"]').text()).toContain('The IPSC server could not start with the new settings.');

    mockAPI.put.mockResolvedValueOnce({ data: {} });
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="apply-errors"]').exists()).toBe(false);
  });
});
