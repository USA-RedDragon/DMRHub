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

import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import SetupWizard from '@/views/SetupWizard.vue';
import { tabPaths, TABS } from '@/lib/setupMenu';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const A = 'a41c00000000000a';
const B = '7f3c00000000000b';

const plain = { editable: true, locked: false, start_only: false, source: 'default', not_in_effect: false, disagree: false };
const boot = { ...plain, editable: false, start_only: true };
const closedSetting = { ...plain, editable: false };

const databasePaths = tabPaths(TABS.find((t) => t.id === 'database')!);

const config = (over: Record<string, unknown> = {}) => ({
  'network-name': 'DMRHub',
  'log-level': 'info',
  http: { 'canonical-host': '', bind: '[::]', port: 3005 },
  database: { driver: 'sqlite', database: 'DMRHub.db' },
  smtp: {}, redis: {}, metrics: {}, pprof: {},
  dmr: {
    mmdvm: { bind: '[::]', port: 62031 },
    modem: { enabled: true, port: '/dev/ttyACM0' },
    transcoding: { 'ambe-servers': [{ address: 'ambe.example:2460', calls: 1 }] },
  },
  ...over,
});

const settings = (extra: Record<string, unknown> = {}) => ({
  ...Object.fromEntries(databasePaths.map((p) => [p, boot])),
  'network-name': { ...plain, default: 'DMRHub' },
  'log-level': { ...plain, default: 'info' },
  'secret': { ...boot, source: 'generated' },
  'http.canonical-host': { ...plain, default: '' },
  'http.bind': { ...boot, default: '[::]' },
  'dmr.mmdvm.port': { ...plain, default: 62031 },
  'dmr.modem.enabled': { ...plain, default: false },
  'dmr.modem.port': { ...plain, default: '' },
  'dmr.transcoding.ambe-servers': { ...plain, default: [] },
  ...extra,
});

const setupResponse = (extra: Record<string, unknown> = {}, cfg = config()) => ({
  data: {
    ...cfg,
    secrets: { secretSet: true, passwordSaltSet: true, smtpPasswordSet: false },
    revision: 0,
    disagree: false,
    settings: settings(),
    replicas: [{ id: A, self: true, revision: 0, current: true, at: '2026-11-01T17:41:07Z', apply_errors: [], invalid: [] }],
    wizard: { phase: 'setup' },
    ...extra,
  },
});

const databaseResponse = (extra: Record<string, unknown> = {}) => setupResponse({
  replicas: [],
  settings: {
    ...Object.fromEntries(databasePaths.map((p) => [p, { ...plain, start_only: true }])),
    'network-name': closedSetting,
    'http.canonical-host': closedSetting,
  },
  wizard: { phase: 'database', database_error: 'dial tcp: connection refused', database_file: '/app/dmrhub-database.json' },
  ...extra,
}, config({ database: { driver: 'postgres', host: 'db.example', port: 5432, database: 'dmrhub' } }));

const missingURL = { valid: false, errors: { http: { 'canonical-host': 'canonical host is required for generating absolute URLs in the HTTP server' } } };

const blank = { template: '<div />' };

let router: Router;

const serve = (config: () => unknown, wizard: () => Promise<unknown> = () => Promise.resolve({ data: { setupwizard: true } })) => {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/setupwizard') {
      return wizard();
    }
    if (url === '/config') {
      return Promise.resolve(config());
    }
    return Promise.reject(new Error(`no ${url}`));
  });
};

const mountAt = async (url = '/setup'): Promise<{ wrapper: VueWrapper; toast: { add: Mock } }> => {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/setup', component: SetupWizard }, { path: '/setup/user', component: blank }],
  });
  await router.push(url);
  await router.isReady();
  const toast = { add: vi.fn() };
  const wrapper = mount(SetupWizard, { global: { plugins: [router], mocks: { $toast: toast } }, attachTo: document.body });
  await flushPromises();
  return { wrapper, toast };
};

const go = async (hash: string) => {
  await router.push(`/setup${hash}`);
  await flushPromises();
};

const save = async (wrapper: VueWrapper) => {
  await wrapper.find('[data-test="save"]').trigger('click');
  await flushPromises();
};

beforeEach(() => {
  vi.clearAllMocks();
  document.body.innerHTML = '';
  window.scrollTo = vi.fn() as unknown as typeof window.scrollTo;
  serve(() => setupResponse());
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  mockAPI.put.mockResolvedValue({ data: { revision: 1, replicas: [{ id: A, self: true, answered: true }], applyErrors: [] } });
});

describe('Setup wizard menu', () => {
  it('opens on the Database tab, with every tab of the Setup menu', async () => {
    const { wrapper } = await mountAt();
    expect(wrapper.find('h1').text()).toBe('Set up DMRHub');
    expect(wrapper.text()).toContain('Check each area, then save. You can change all of it later in Admin, Setup.');
    const menu = wrapper.findAll('[data-test="setup-menu"] a').map((a) => a.attributes('data-test'));
    expect(menu[0]).toBe('menu-database');
    expect(menu).toContain('menu-general');
    expect(menu).toContain('menu-modem');
    expect(menu).toContain('menu-plugins');
    expect(wrapper.find('[data-test="tab-database"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="save"]').text()).toBe('Save and continue');
    expect(wrapper.text()).not.toMatch(/\bdmr\.|canonical-host|start_only/);
    wrapper.unmount();
  });

  it('says that DMRHub reaches the database once it does, and keeps the database read-only', async () => {
    const { wrapper } = await mountAt();
    expect(wrapper.find('[data-test="card-Database"] [data-test="database-reached"]').text()).toBe('Connected.');
    expect(wrapper.find('[data-test="database-test"]').exists()).toBe(false);
    expect((wrapper.find('#setup-database-driver').element as HTMLSelectElement).disabled).toBe(true);
    expect(wrapper.find('#setup-database-host').exists()).toBe(false);
    wrapper.unmount();
  });

  it('marks the tab of a missing Site URL with a red dot and says where to set it', async () => {
    mockAPI.post.mockResolvedValue({ data: missingURL });
    const { wrapper } = await mountAt();
    expect(mockAPI.post).toHaveBeenCalledWith('/config/validate', expect.objectContaining({ 'network-name': 'DMRHub' }));
    expect(wrapper.find('[data-test="menu-error-http"]').exists()).toBe(true);
    expect(wrapper.findAll('[data-test^="menu-error-"]')).toHaveLength(1);
    expect(wrapper.find('[data-test="savebar-message"]').text()).toBe('Set the Site URL on the HTTP tab.');
    await go('#http');
    expect(wrapper.find('[data-test="field-http.canonical-host"] [data-test="field-error"]').text()).toBe('Required.');

    mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
    await wrapper.find('#setup-http-canonical-host').setValue('https://dmrhub.example.org');
    expect(wrapper.find('[data-test="menu-error-http"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="savebar-message"]').text()).toBe('');
    wrapper.unmount();
  });

  it('goes to the tab of a missing value instead of saving', async () => {
    const { wrapper } = await mountAt('/setup#general');
    await save(wrapper);
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(router.currentRoute.value.hash).toBe('#http');
    wrapper.unmount();
  });

  it('saves every tab at once, then opens the first admin page', async () => {
    const { wrapper, toast } = await mountAt('/setup#general');
    await wrapper.find('#setup-network-name').setValue('N6TEST Network');
    await go('#http');
    await wrapper.find('#setup-http-canonical-host').setValue('https://dmrhub.example.org');
    await save(wrapper);
    expect(mockAPI.put).toHaveBeenCalledTimes(1);
    const sent = mockAPI.put.mock.calls[0]![1];
    expect(sent['network-name']).toBe('N6TEST Network');
    expect(sent.http['canonical-host']).toBe('https://dmrhub.example.org');
    expect(sent).not.toHaveProperty('secret');
    expect(sent.dmr.modem.port).toBe('/dev/ttyACM0');
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
    expect(router.currentRoute.value.path).toBe('/setup/user');
    wrapper.unmount();
  });

  it('shows the errors of the server under their fields and on their tabs', async () => {
    const { wrapper } = await mountAt('/setup#http');
    await wrapper.find('#setup-http-canonical-host').setValue('https://dmrhub.example.org');
    mockAPI.post.mockResolvedValue({ data: { valid: false, errors: { smtp: { from: "SMTP 'from' address is required when SMTP is enabled" }, dmr: { mmdvm: { port: 'invalid DMR MMDVM port provided' } } } } });
    await save(wrapper);
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="menu-error-smtp"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="menu-error-dmr"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="savebar-message"]').text()).toBe('Fix the marked settings on the SMTP and DMR and MMDVM tabs.');
    expect(router.currentRoute.value.hash).toBe('#smtp');
    expect(wrapper.text()).toContain("SMTP 'from' address is required when SMTP is enabled");
    wrapper.unmount();
  });

  it('opens the first admin page at once when the settings are complete', async () => {
    serve(() => setupResponse(), () => Promise.resolve({ data: { setupwizard: true, config_valid: true, first_admin_open: true } }));
    await mountAt();
    expect(router.currentRoute.value.path).toBe('/setup/user');
  });

  it('leaves out what needs the running network: the modem test and pill, and the AMBE server status', async () => {
    const { wrapper } = await mountAt('/setup#modem');
    expect(wrapper.find('[data-test="modem-pill"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="card-Test the modem"]').exists()).toBe(false);
    await go('#transcoding');
    expect(wrapper.find('[data-test="ambe-server-address-0"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ambe-server-status-0"]').exists()).toBe(false);
    const asked = mockAPI.get.mock.calls.map((c) => c[0]);
    expect(asked).not.toContain('/config/modem');
    expect(asked).not.toContain('/config/transcoding');
    wrapper.unmount();
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
    serve(() => setupResponse({}, config({ http: { 'canonical-host': 'https://dmrhub.example.org' } })));
    const { wrapper } = await mountAt();
    await save(wrapper);
    const alert = wrapper.find('[data-test="apply-errors"]');
    expect(alert.text()).toContain('The settings were saved, but replica 7f3c could not put all of them into effect.');
    expect(alert.text()).toContain('The MMDVM server of replica 7f3c could not start with the new settings. It keeps running with its previous settings.');
    expect(alert.text()).toContain('Replica a41c runs with the new settings.');
    expect(router.currentRoute.value.path).toBe('/setup');
    wrapper.unmount();
  });

  it('warns that the replicas differ', async () => {
    serve(() => setupResponse({
      disagree: true,
      settings: settings({ 'dmr.mmdvm.port': { ...plain, disagree: true } }),
    }));
    const { wrapper } = await mountAt();
    expect(wrapper.find('[data-test="replicas-differ"]').text()).toContain('1 setting differs between replicas: Port (MMDVM).');
    wrapper.unmount();
  });
});

describe('Setup wizard database phase', () => {
  it('opens only the Database tab while DMRHub cannot reach its database', async () => {
    serve(() => databaseResponse());
    const { wrapper } = await mountAt();
    const banner = wrapper.find('[data-test="database-problem"]');
    expect(banner.text()).toContain('DMRHub cannot reach its database.');
    expect(banner.text()).toContain('Fix the settings on the Database tab and save.');
    expect(banner.text()).toContain('/app/dmrhub-database.json');
    expect(banner.text()).toContain('dial tcp: connection refused');
    expect(mockAPI.post).not.toHaveBeenCalled();
    expect((wrapper.find('#setup-database-host').element as HTMLInputElement).disabled).toBe(false);
    expect(wrapper.find('[data-test="menu-error-http"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="card-Database"] [data-test="database-test"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="tab-database"]').text()).not.toContain('Read at start');
    expect(wrapper.find('[data-test="tab-database"]').text()).not.toContain('DMRHub reads this at start');
    await go('#general');
    expect((wrapper.find('#setup-network-name').element as HTMLInputElement).disabled).toBe(true);
    wrapper.unmount();
  });

  it('hides the server fields while SQLite is picked', async () => {
    serve(() => databaseResponse());
    const { wrapper } = await mountAt();
    expect(wrapper.find('#setup-database-host').exists()).toBe(true);
    await wrapper.find('#setup-database-driver').setValue('sqlite');
    for (const id of ['host', 'port', 'username', 'password']) {
      expect(wrapper.find(`#setup-database-${id}`).exists()).toBe(false);
    }
    expect(wrapper.find('#setup-database-database').exists()).toBe(true);
    wrapper.unmount();
  });

  it('tests the connection with the settings on the page and saves nothing', async () => {
    serve(() => databaseResponse());
    const { wrapper } = await mountAt();
    await wrapper.find('#setup-database-host').setValue('db2.example');
    mockAPI.post.mockResolvedValue({ data: { reached: true } });
    await wrapper.find('[data-test="database-test"] button').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/setupwizard/database-test', expect.objectContaining({ database: expect.objectContaining({ host: 'db2.example' }) }));
    expect(wrapper.find('[data-test="database-test-result"]').text()).toBe('Connected.');
    expect(mockAPI.put).not.toHaveBeenCalled();

    mockAPI.post.mockResolvedValue({ data: { reached: false, error: 'DMRHub cannot reach this database: connection refused' } });
    await wrapper.find('[data-test="database-test"] button').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="database-test-result"]').text()).toBe('DMRHub cannot reach this database: connection refused');
    await wrapper.find('#setup-database-host').setValue('db3.example');
    expect(wrapper.find('[data-test="database-test-result"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('saves the database and opens the other tabs once DMRHub reaches it', async () => {
    serve(() => databaseResponse());
    const { wrapper, toast } = await mountAt();
    mockAPI.put.mockResolvedValue({ data: { message: 'DMRHub reached the database', phase: 'setup' } });
    serve(() => setupResponse());
    await save(wrapper);
    expect(mockAPI.put).toHaveBeenCalledWith('/config', expect.objectContaining({ database: expect.objectContaining({ host: 'db.example' }) }));
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success', detail: 'DMRHub reached its database. Check the other settings and save.' }));
    expect(wrapper.find('[data-test="database-problem"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="database-reached"]').exists()).toBe(true);
    expect(router.currentRoute.value.path).toBe('/setup');
    wrapper.unmount();
  });

  it('shows why a database setting failed under its field', async () => {
    serve(() => databaseResponse());
    const { wrapper } = await mountAt();
    mockAPI.put.mockRejectedValue({
      response: { status: 400, data: { error: 'DMRHub cannot reach this database: no such host', errors: { database: { host: 'DMRHub cannot reach this database: no such host' } } } },
    });
    await save(wrapper);
    expect(wrapper.find('[data-test="field-database.host"] [data-test="field-error"]').text()).toBe('DMRHub cannot reach this database: no such host');
    expect(wrapper.find('[data-test="menu-error-database"]').exists()).toBe(true);
    wrapper.unmount();
  });

  const fixWith = async (
    answers: Array<() => Promise<unknown>>,
    health: () => Promise<unknown> = () => Promise.reject(new Error('Network Error')),
    maxTimers = Infinity,
  ) => {
    serve(() => databaseResponse());
    const { wrapper, toast } = await mountAt();
    const polls: string[] = [];
    mockAPI.get.mockImplementation((url: string) => {
      if (url === '/healthcheck') {
        return health();
      }
      if (url === '/config') {
        return Promise.resolve(setupResponse());
      }
      polls.push(url);
      const next = answers.shift();
      return next ? next() : Promise.reject(new Error('no more answers'));
    });
    mockAPI.put.mockResolvedValue({ status: 202, data: { status: 'pending' } });
    vi.useFakeTimers();
    try {
      await wrapper.find('[data-test="save"]').trigger('click');
      await flushPromises();
      for (let i = 0; i < maxTimers + 61 && vi.getTimerCount() > 0; i++) {
        await vi.runOnlyPendingTimersAsync();
        await flushPromises();
      }
    } finally {
      vi.useRealTimers();
    }
    return { wrapper, toast, polls };
  };

  it('waits for the migration of a database fix and then opens the other settings', async () => {
    const pending = () => Promise.resolve({ data: { status: 'pending' } });
    const { toast, polls } = await fixWith([pending, pending, () => Promise.resolve({ data: { status: 'done', phase: 'setup' } })]);
    expect(polls).toEqual(Array(3).fill('/setupwizard/database-fix'));
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success', detail: 'DMRHub reached its database. Check the other settings and save.' }));
    expect(mockAPI.get).toHaveBeenLastCalledWith('/config');
  });

  it('shows the error of a database fix whose migration failed', async () => {
    const { wrapper, toast, polls } = await fixWith([() => Promise.resolve({ data: { status: 'failed', error: 'DMRHub cannot migrate this database: x' } })]);
    expect(polls).toHaveLength(1);
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'error', detail: 'Failed to save the settings' }));
    expect(toast.add).not.toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
    expect(wrapper.find('[data-test="savebar-message"]').text()).toBe('The settings were not saved: DMRHub cannot migrate this database: x');
  });

  it('goes to DMRHub once the setup wizard stops during a database fix and DMRHub answers', async () => {
    const { toast, polls } = await fixWith([
      () => Promise.resolve({ data: { status: 'pending' } }),
      () => Promise.reject(new Error('Network Error')),
    ], () => Promise.resolve({ data: { status: 'healthy' } }));
    expect(polls).toHaveLength(2);
    expect(mockAPI.get).toHaveBeenCalledWith('/healthcheck');
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success', detail: 'DMRHub reached its database and is starting' }));
  });

  it('keeps asking, then says so, when neither the setup wizard nor DMRHub answers during a database fix', async () => {
    const lost = () => Promise.reject(new Error('Network Error'));
    const health = vi.fn(lost);
    const { wrapper, toast, polls } = await fixWith([() => Promise.resolve({ data: { status: 'pending' } }), ...Array(100).fill(lost)], health, 10);
    expect(polls).toHaveLength(61);
    expect(health.mock.calls.length).toBeGreaterThan(60);
    expect(wrapper.find('[data-test="fix-lost"]').text()).toContain('DMRHub did not answer after the database fix.');
    expect(toast.add).not.toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
  });

  it('goes to DMRHub when it answers after the page said it did not', async () => {
    const lost = () => Promise.reject(new Error('Network Error'));
    let asks = 0;
    const health = () => (++asks > 63 ? Promise.resolve({ data: { status: 'healthy' } }) : lost());
    const { wrapper, toast, polls } = await fixWith([() => Promise.resolve({ data: { status: 'pending' } }), ...Array(100).fill(lost)], health);
    expect(polls).toHaveLength(61);
    expect(asks).toBe(64);
    expect(wrapper.find('[data-test="fix-lost"]').exists()).toBe(false);
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success', detail: 'DMRHub reached its database and is starting' }));
  });

  it('follows the fix again when the setup wizard answers after a miss', async () => {
    const { toast, polls } = await fixWith([
      () => Promise.reject(new Error('Network Error')),
      () => Promise.resolve({ data: '<!doctype html>' }),
      () => Promise.resolve({ data: { status: 'done', phase: 'setup' } }),
    ], () => Promise.resolve({ status: 503, data: { status: 'not ready' } }));
    expect(polls).toHaveLength(3);
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success', detail: 'DMRHub reached its database. Check the other settings and save.' }));
  });

  it('says why the wizard cannot keep the database setting', async () => {
    serve(() => databaseResponse({
      wizard: { phase: 'database', database_error: 'x', database_file: '/app/dmrhub-database.json', store_error: 'DMRHub cannot write in /app' },
    }));
    const { wrapper } = await mountAt();
    expect(wrapper.find('[data-test="database-problem"]').text()).toContain(
      'The setup wizard cannot keep a database setting here: DMRHub cannot write in /app. Set the database in the config file or the environment of the deployment, then restart DMRHub.');
    wrapper.unmount();
  });
});

describe('Setup wizard that cannot change the settings', () => {
  it('says to fix the config and restart once a database setting was saved', async () => {
    serve(() => setupResponse(), () => Promise.resolve({ data: { setupwizard: true, closed: 'database' } }));
    const { wrapper } = await mountAt();
    const card = wrapper.find('[data-test="wizard-closed"]');
    expect(card.text()).toContain('DMRHub cannot reach its database.');
    expect(card.text()).toContain('An admin must fix the database in the config file, the environment or the flags, then restart DMRHub.');
    expect(wrapper.find('[data-test="setup-menu"]').exists()).toBe(false);
    expect(mockAPI.get).not.toHaveBeenCalledWith('/config');
    wrapper.unmount();
  });

  it('says to fix the config and restart once an admin exists', async () => {
    serve(() => setupResponse(), () => Promise.resolve({ data: { setupwizard: true, closed: 'settings' } }));
    const { wrapper } = await mountAt();
    const card = wrapper.find('[data-test="wizard-closed"]');
    expect(card.text()).toContain('DMRHub has an admin, so the setup wizard cannot change the settings.');
    expect(wrapper.find('[data-test="setup-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('shows the same page when the settings refuse the wizard', async () => {
    mockAPI.get.mockImplementation((url: string) => (url === '/setupwizard'
      ? Promise.resolve({ data: { setupwizard: true } })
      : Promise.reject({ response: { status: 409, data: { error: 'x', closed: 'settings' } } })));
    const { wrapper } = await mountAt();
    expect(wrapper.find('[data-test="wizard-closed"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="setup-menu"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
