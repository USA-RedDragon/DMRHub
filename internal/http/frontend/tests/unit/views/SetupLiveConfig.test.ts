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

const A = 'a41c00000000000a';
const B = '7f3c00000000000b';

const plain = { editable: true, locked: false, start_only: false, source: 'default', not_in_effect: false, disagree: false };
const deployment = { ...plain, editable: false, start_only: true };

const response = (extra: Record<string, unknown> = {}) => ({
  data: {
    'network-name': 'W5DMR Network',
    'log-level': 'debug',
    dmr: {
      mmdvm: { bind: '[::]', port: 62040 },
      ipsc: { enabled: true, bind: '[::]', port: 50000, 'network-id': 311860 },
      openbridge: {},
      retention: {},
    },
    http: {}, database: { driver: 'postgres', host: 'postgres.dmrhub.svc' }, smtp: {}, redis: {}, metrics: {}, pprof: {},
    secrets: { secretSet: true, passwordSaltSet: true, smtpPasswordSet: false },
    revision: 14,
    disagree: true,
    settings: {
      'network-name': { ...plain, source: 'setup', saved: 'W5DMR Network', default: 'DMRHub' },
      'log-level': { ...plain, editable: false, locked: true, source: 'env', saved: 'info' },
      'secret': { ...deployment, source: 'generated' },
      'database.host': { ...deployment },
      'database.driver': { ...deployment },
      'dmr.ipsc.port': { ...plain, editable: false, locked: true, source: 'env' },
      'dmr.ipsc.network-id': {
        ...plain, source: 'setup', saved: 311860, disagree: true,
        replicas: [
          { replica: A, self: true, value: 311860, source: 'setup' },
          { replica: B, self: false, value: 777777, source: 'env' },
        ],
      },
      'dmr.mmdvm.port': {
        ...plain, source: 'setup', saved: 62040, default: 62031, disagree: true,
        replicas: [
          { replica: A, self: true, value: 62040, source: 'setup' },
          { replica: B, self: false, value: 62031, source: 'setup' },
        ],
      },
    },
    replicas: [
      { id: A, self: true, revision: 14, current: true, at: '2026-10-26T17:41:07Z', apply_errors: [], invalid: [] },
      { id: B, self: false, revision: 14, current: true, at: '2026-10-26T17:41:08Z',
        apply_errors: [{ server: 'mmdvm', running: true, error: 'bind: address already in use' }], invalid: [] },
    ],
    ...extra,
  },
});

const mountSetup = async (path = '/admin/setup', data = response()) => {
  mockAPI.get.mockImplementation((url: string) => (url === '/config' ? Promise.resolve(data) : Promise.reject(new Error('no'))));
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  const toast = { add: vi.fn() };
  const wrapper = mount(SetupWizard, {
    attachTo: document.body,
    global: {
      mocks: { $route: { path, query: { token: 't' } }, $router: { push: vi.fn() }, $toast: toast },
    },
  });
  await flushPromises();
  return { wrapper, toast };
};

const keys = /\bdmr\.|network-id|log-level|canonical-host|not_in_effect|start_only/;

describe('Setup page with live settings', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('makes a locked setting read-only and says that the deployment sets it', async () => {
    const { wrapper } = await mountSetup();
    const level = wrapper.find('#log-level');
    expect((level.element as HTMLSelectElement).disabled).toBe(true);
    const label = wrapper.find('label[for="log-level"]');
    expect(label.text()).toContain('Set by the deployment');
    expect(wrapper.text()).toContain('The environment of the deployment sets this. Change it there and restart DMRHub.');
    expect(wrapper.text()).toContain('A value saved in Setup, info, comes back if the deployment stops setting it.');
    expect((wrapper.find('#ipsc-port').element as HTMLInputElement).readOnly).toBe(true);
    expect((wrapper.find('#network-name').element as HTMLInputElement).readOnly).toBe(false);
  });

  it('marks a setting saved in Setup and names its default', async () => {
    const { wrapper } = await mountSetup();
    expect(wrapper.find('label[for="network-name"]').text()).toContain('Saved in Setup');
    expect(wrapper.text()).toContain('Set it back to the default, DMRHub, to follow the default.');
  });

  it('says that DMRHub made the secret', async () => {
    const { wrapper } = await mountSetup();
    expect((wrapper.find('#secret').element as HTMLInputElement).readOnly).toBe(true);
    expect(wrapper.text()).toContain('DMRHub made a random value at its first start and keeps it in the database.');
  });

  it('warns that the replicas differ and lists the value of each', async () => {
    const { wrapper } = await mountSetup();
    const banner = wrapper.find('[data-test="replicas-differ"]');
    expect(banner.text()).toContain('The replicas do not run with the same settings.');
    expect(banner.text()).toContain('2 settings differ between replicas: Network ID (IPSC) and Port (MMDVM).');
    const lists = wrapper.findAll('[data-test="setting-replicas"]');
    expect(lists.length).toBe(2);
    const id = lists.find((l) => l.text().includes('777777'))!;
    expect(id.text()).toContain('7f3c');
    expect(id.text()).toContain('set by its environment');
    const port = lists.find((l) => l.text().includes('62031'))!;
    expect(port.text()).toContain('Not in effect');
    expect(wrapper.find('label[for="mmdvm-port"]').text()).toContain('Replicas differ');
  });

  it('shows the replicas with their state', async () => {
    const { wrapper } = await mountSetup();
    const table = wrapper.find('[data-test="replicas"]');
    expect(table.text()).toContain('a41c (this one)');
    expect(table.text()).toContain('#14, latest');
    expect(table.text()).toContain('All settings in effect');
    expect(table.text()).toContain('MMDVM server kept its previous settings');
  });

  it('names the replica that could not put a save into effect', async () => {
    mockAPI.put.mockRejectedValue({
      response: {
        status: 409,
        data: {
          error: 'Some settings could not be applied',
          replicas: [{ id: A, self: true, answered: true }, { id: B, self: false, answered: true }],
          applyErrors: [{ replica: B, server: 'mmdvm', running: true, error: 'listen udp [::]:62040: bind: address already in use' }],
        },
      },
    });
    const { wrapper } = await mountSetup();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const alert = wrapper.find('[data-test="apply-errors"]');
    expect(alert.text()).toContain('The settings were saved, but replica 7f3c could not put all of them into effect.');
    expect(alert.text()).toContain('The MMDVM server of replica 7f3c could not start with the new settings. It keeps running with its previous settings.');
    expect(alert.text()).toContain('Replica a41c runs with the new settings.');
  });

  it('shows no config key', async () => {
    const { wrapper } = await mountSetup();
    expect(wrapper.text()).not.toMatch(keys);
  });

  it('opens only the database settings while DMRHub cannot reach its database', async () => {
    const data = response({
      disagree: false,
      replicas: [],
      settings: {
        'network-name': { ...plain, editable: false },
        'database.host': { ...plain, editable: true, start_only: true },
        'database.driver': { ...plain, editable: true, start_only: true },
      },
      wizard: { phase: 'database', database_error: 'dial tcp: connection refused', database_file: '/app/dmrhub-database.json' },
    });
    const { wrapper } = await mountSetup('/setup', data);
    const banner = wrapper.find('[data-test="database-problem"]');
    expect(banner.text()).toContain('DMRHub cannot reach its database.');
    expect(banner.text()).toContain('/app/dmrhub-database.json');
    expect(banner.text()).toContain('dial tcp: connection refused');
    expect((wrapper.find('#network-name').element as HTMLInputElement).readOnly).toBe(true);
    expect((wrapper.find('#database-host').element as HTMLInputElement).readOnly).toBe(false);

    mockAPI.put.mockResolvedValue({ data: { message: 'DMRHub reached the database', phase: 'setup' } });
    const before = mockAPI.get.mock.calls.length;
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.get.mock.calls.length).toBe(before + 1);
  });

  it('says why the wizard cannot keep the database setting', async () => {
    const data = response({
      disagree: false, replicas: [],
      wizard: { phase: 'database', database_error: 'x', database_file: '/app/dmrhub-database.json', store_error: 'DMRHub cannot write in /app' },
    });
    const { wrapper } = await mountSetup('/setup', data);
    expect(wrapper.find('[data-test="database-problem"]').text()).toContain(
      'The setup wizard cannot keep a database setting here: DMRHub cannot write in /app. Set the database in the config file or the environment of the deployment, then restart DMRHub.');
  });

  const mountClosed = async (wizard: Promise<unknown>, config: Promise<unknown>) => {
    mockAPI.get.mockImplementation((url: string) => (url === '/setupwizard' ? wizard : config));
    const wrapper = mount(SetupWizard, {
      attachTo: document.body,
      global: { mocks: { $route: { path: '/setup', query: {} }, $router: { push: vi.fn(), replace: vi.fn() }, $toast: { add: vi.fn() } } },
    });
    await flushPromises();
    return wrapper;
  };

  it('says to fix the config and restart once a database setting was saved', async () => {
    const wrapper = await mountClosed(Promise.resolve({ data: { setupwizard: true, closed: 'database' } }), Promise.resolve(response()));
    const card = wrapper.find('[data-test="wizard-closed"]');
    expect(card.text()).toContain('DMRHub cannot reach its database.');
    expect(card.text()).toContain('An admin must fix the database in the config file, the environment or the flags, then restart DMRHub.');
    expect(wrapper.find('form').exists()).toBe(false);
    expect(mockAPI.get).not.toHaveBeenCalledWith('/config');
  });

  it('says to fix the config and restart once an admin exists', async () => {
    const wrapper = await mountClosed(Promise.resolve({ data: { setupwizard: true, closed: 'settings' } }), Promise.resolve(response()));
    const card = wrapper.find('[data-test="wizard-closed"]');
    expect(card.text()).toContain('DMRHub has an admin, so the setup wizard cannot change the settings.');
    expect(card.text()).toContain('then restart DMRHub.');
    expect(wrapper.find('form').exists()).toBe(false);
  });

  it('shows the same page when the settings refuse the wizard', async () => {
    const refused = Promise.reject({ response: { status: 409, data: { error: 'x', closed: 'settings' } } });
    refused.catch(() => {});
    const wrapper = await mountClosed(Promise.resolve({ data: { setupwizard: true } }), refused);
    expect(wrapper.find('[data-test="wizard-closed"]').exists()).toBe(true);
    expect(wrapper.find('form').exists()).toBe(false);
  });
});
