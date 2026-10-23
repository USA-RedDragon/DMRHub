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

const everythingOn = () => ({
  data: {
    dmr: {
      mmdvm: { bind: '[::]', port: 62031 },
      ipsc: { enabled: true, bind: '[::]', port: 50000, 'network-id': 311860 },
      openbridge: { enabled: true, bind: '[::]', port: 62035 },
      retention: { messages: 0, transmissions: 0, bursts: 0 },
    },
    http: { bind: '[::]', port: 3005, cors: { enabled: true }, 'robots-txt': { mode: 'custom' } },
    database: { driver: 'postgres', host: 'db', port: 5432 },
    smtp: { enabled: true, port: 25 },
    redis: { enabled: true, port: 6379 },
    metrics: { enabled: true, port: 9000 },
    pprof: { enabled: true, port: 6060 },
  },
});

const mountWizard = (toast = { add: vi.fn() }) => mount(SetupWizard, {
  attachTo: document.body,
  global: {
    mocks: { $route: { path: '/admin/setup', query: {} }, $router: { push: vi.fn() }, $toast: toast },
  },
});

describe('Setup page form', { timeout: 20000 }, () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('gives every field its own id, and each label points at a field in its own card', async () => {
    mockAPI.get.mockResolvedValue(everythingOn());
    const wrapper = mountWizard();
    await flushPromises();
    const ids = Array.from(document.querySelectorAll('form [id]')).map((e) => e.id);
    const dupes = ids.filter((id, i) => ids.indexOf(id) !== i);
    expect(dupes).toEqual([]);
    const misplaced = Array.from(document.querySelectorAll('form label[for]')).filter((label) => {
      const target = document.getElementById(label.getAttribute('for')!);
      return !target || target.closest('[data-slot="card"]') !== label.closest('[data-slot="card"]');
    }).map((label) => label.getAttribute('for'));
    expect(misplaced).toEqual([]);
    wrapper.unmount();
  });

  it('sends every port and the IPSC network ID as numbers, also when a field is cleared', async () => {
    mockAPI.get.mockResolvedValue(everythingOn());
    mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
    mockAPI.put.mockResolvedValue({ data: {} });
    const wrapper = mountWizard();
    await flushPromises();
    const numbers = wrapper.findAll('form input[type="number"]');
    expect(numbers.length).toBe(11);
    for (const input of numbers) {
      await input.setValue('');
    }
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const payload = mockAPI.post.mock.calls[0]![1] as Record<string, Record<string, Record<string, unknown>>>;
    const sent = {
      mmdvm: payload.dmr!.mmdvm!.port,
      ipscPort: payload.dmr!.ipsc!.port,
      ipscNetworkID: payload.dmr!.ipsc!['network-id'],
      openbridgePort: payload.dmr!.openbridge!.port,
      openbridgeServerID: payload.dmr!.openbridge!['server-id'],
      http: payload.http!.port,
      database: payload.database!.port,
      smtp: payload.smtp!.port,
      redis: payload.redis!.port,
      metrics: payload.metrics!.port,
      pprof: payload.pprof!.port,
    };
    expect(sent).toEqual({
      mmdvm: 0, ipscPort: 0, ipscNetworkID: 0, openbridgePort: 0, openbridgeServerID: 0,
      http: 0, database: 0, smtp: 0, redis: 0, metrics: 0, pprof: 0,
    });
    wrapper.unmount();
  });

  it('shows an error when the server refuses the settings', async () => {
    mockAPI.get.mockResolvedValue(everythingOn());
    mockAPI.post.mockRejectedValue(new Error('Request failed with status code 400'));
    const toast = { add: vi.fn() };
    const wrapper = mountWizard(toast);
    await flushPromises();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({
      severity: 'error',
      detail: 'Failed to save configuration',
    }));
    wrapper.unmount();
  });

  it('shows an error when the settings cannot be loaded', async () => {
    mockAPI.get.mockRejectedValue(new Error('Request failed with status code 500'));
    const toast = { add: vi.fn() };
    const wrapper = mountWizard(toast);
    await flushPromises();
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({
      severity: 'error',
      detail: 'Failed to load configuration',
    }));
    wrapper.unmount();
  });
});
