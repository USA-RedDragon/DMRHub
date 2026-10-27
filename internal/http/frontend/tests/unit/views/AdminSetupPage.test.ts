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
import SetupPage from '@/views/admin/SetupPage.vue';
import { tabPaths, TABS } from '@/lib/setupMenu';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const plain = { editable: true, locked: false, start_only: false, source: 'default', not_in_effect: false, disagree: false };
const boot = { ...plain, editable: false, start_only: true };
const locked = (source: string, origin?: string) => ({ ...plain, editable: false, locked: true, source, origin });

const config = (over: Record<string, unknown> = {}) => ({
  'network-name': 'Example DMR Network',
  'log-level': 'info',
  http: { 'canonical-host': 'https://dmrhub.example.org', bind: '[::]', port: 3005, 'trusted-proxies': ['10.42.0.0/16'] },
  database: { driver: 'postgres', host: 'postgres.dmrhub.svc' },
  smtp: {}, redis: {}, metrics: {}, pprof: {},
  dmr: {
    mmdvm: { bind: '[::]', port: 62031 },
    ipsc: { enabled: true, bind: '[::]', port: 50000, 'network-id': 3000001 },
    transcoding: { 'ambe-servers': [] },
  },
  ...over,
});

const settings = (extra: Record<string, unknown> = {}) => ({
  ...Object.fromEntries(tabPaths(TABS.find((t) => t.id === 'database')!).map((p) => [p, boot])),
  'network-name': { ...plain, source: 'setup', saved: 'Example DMR Network', default: 'DMRHub' },
  'log-level': { ...plain, default: 'info' },
  'auto-approve-users': { ...plain, default: false },
  'secret': { ...boot, locked: true, source: 'env', origin: 'SECRET' },
  'http.canonical-host': { ...plain, source: 'setup', default: '' },
  'http.bind': { ...boot, default: '[::]' },
  'http.port': { ...locked('flag', '--http.port'), start_only: true },
  'http.trusted-proxies': { ...locked('file'), start_only: true },
  'dmr.mmdvm.port': locked('file'),
  'dmr.mmdvm.bind': { ...plain, default: '[::]' },
  'dmr.public-address': { ...plain, default: '' },
  'dmr.ipsc.enabled': { ...plain, source: 'setup', default: false },
  'dmr.ipsc.port': { ...plain, default: 50000 },
  'dmr.ipsc.network-id': { ...plain, source: 'setup', default: 0 },
  'dmr.transcoding.ambe-servers': { ...plain, default: [] },
  ...extra,
});

const response = (cfg = config(), extra: Record<string, unknown> = {}) => ({
  data: {
    ...cfg,
    secrets: { secretSet: true, passwordSaltSet: true, smtpPasswordSet: false },
    revision: 41,
    disagree: false,
    settings: settings(extra),
    replicas: [
      { id: 'a41c00000000000a', self: true, revision: 41, current: true, at: '2026-10-27T07:41:07Z', apply_errors: [], invalid: [] },
      { id: '7f3c00000000000b', self: false, revision: 41, current: true, at: '2026-10-27T07:41:08Z', apply_errors: [], invalid: [] },
    ],
  },
});

const getConfig = (res: () => unknown) => {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/config/transcoding') {
      return Promise.resolve({ data: { servers: [] } });
    }
    return Promise.resolve(res());
  });
};

const blank = { template: '<div />' };

const mountAt = async (url: string): Promise<{ wrapper: VueWrapper; router: Router }> => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/admin/setup', component: SetupPage },
      { path: '/admin/peers', component: blank },
      { path: '/admin/plugins', component: blank },
    ],
  });
  await router.push(url);
  await router.isReady();
  const wrapper = mount(SetupPage, {
    global: { plugins: [router], mocks: { $toast: { add: vi.fn() } } },
    attachTo: document.body,
  });
  await flushPromises();
  return { wrapper, router };
};

const go = async (router: Router, hash: string) => {
  await router.push(`/admin/setup${hash}`);
  await flushPromises();
};

beforeEach(() => {
  vi.clearAllMocks();
  window.scrollTo = vi.fn() as unknown as typeof window.scrollTo;
  getConfig(() => response());
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  mockAPI.put.mockResolvedValue({ data: { revision: 42, replicas: [], applyErrors: [] } });
});

describe('Admin Setup page tabs', () => {
  it('opens the tab of the URL hash and marks it in the menu', async () => {
    const { wrapper } = await mountAt('/admin/setup#ipsc');
    expect(wrapper.find('[data-test="tab-ipsc"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="menu-ipsc"]').attributes('aria-current')).toBe('page');
    expect(wrapper.find('[data-test="menu-general"]').attributes('aria-current')).toBeUndefined();
    expect(wrapper.text()).toContain('/admin/setup#ipsc');
    wrapper.unmount();
  });

  it('follows the menu links to each tab', async () => {
    const { wrapper, router } = await mountAt('/admin/setup');
    expect(wrapper.find('[data-test="tab-general"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="menu-dmr"]').attributes('href')).toBe('/admin/setup#dmr');
    await wrapper.find('[data-test="menu-dmr"]').trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.hash).toBe('#dmr');
    expect(wrapper.find('[data-test="tab-dmr"]').exists()).toBe(true);
    expect(wrapper.find('#setup-dmr-public-address').exists()).toBe(true);
    wrapper.unmount();
  });

  it('opens the first tab for an unknown hash', async () => {
    const { wrapper } = await mountAt('/admin/setup#nothing');
    expect(wrapper.find('[data-test="tab-general"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('shows the private call tab with every private call setting of the server', async () => {
    const pc = ['voice.reach', 'voice.unknown', 'voice.max-age', 'data.reach', 'data.unknown', 'data.max-age'].map((k) => `dmr.private-calls.${k}`);
    getConfig(() => response(config(), Object.fromEntries(pc.map((p) => [p, plain]))));
    const { wrapper } = await mountAt('/admin/setup#pcroute');
    expect(wrapper.find('[data-test="menu-pcroute"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="tab-pcroute"]').exists()).toBe(true);
    for (const p of pc) {
      expect(wrapper.find(`[data-test="field-${p}"]`).exists()).toBe(true);
    }
    wrapper.unmount();
  });

  it('shows the private call tab with the settings that exist', async () => {
    getConfig(() => response(config(), { 'dmr.private-calls.voice.reach': { ...plain, default: 'hybrid' } }));
    const { wrapper } = await mountAt('/admin/setup#pcroute');
    expect(wrapper.find('[data-test="tab-pcroute"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="field-dmr.private-calls.voice.reach"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="field-dmr.private-calls.voice.unknown"]').exists()).toBe(false);
    expect(wrapper.text()).toContain('Hybrid');
    expect(wrapper.text()).not.toContain('dmr.private-calls');
    wrapper.unmount();
  });
});

describe('Admin Setup page dirty tracking', () => {
  it('marks a tab with unsaved changes in the menu until Reset', async () => {
    const { wrapper, router } = await mountAt('/admin/setup#general');
    expect(wrapper.find('[data-test="menu-dirty-general"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="save"]').attributes('disabled')).toBeDefined();
    await wrapper.find('#setup-network-name').setValue('Other');
    expect(wrapper.find('[data-test="menu-dirty-general"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="save"]').attributes('disabled')).toBeUndefined();
    expect(wrapper.find('[data-test="savebar-message"]').text()).toContain('You have unsaved changes on this tab');

    await go(router, '#ipsc');
    expect(wrapper.find('[data-test="menu-dirty-general"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="menu-dirty-ipsc"]').exists()).toBe(false);

    await go(router, '#general');
    expect((wrapper.find('#setup-network-name').element as HTMLInputElement).value).toBe('Other');
    await wrapper.find('[data-test="reset"]').trigger('click');
    expect(wrapper.find('[data-test="menu-dirty-general"]').exists()).toBe(false);
    expect((wrapper.find('#setup-network-name').element as HTMLInputElement).value).toBe('Example DMR Network');
    wrapper.unmount();
  });

  it('saves only the settings of the tab and keeps the edits of the others', async () => {
    const { wrapper, router } = await mountAt('/admin/setup#ipsc');
    await wrapper.find('#setup-dmr-ipsc-port').setValue('50010');
    await go(router, '#general');
    await wrapper.find('#setup-network-name').setValue('Saved Name');

    const saved = config({ 'network-name': 'Saved Name' });
    getConfig(() => response(saved));
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();

    const payload = mockAPI.put.mock.calls[0]![1];
    expect(mockAPI.post.mock.calls[0]![1]).toEqual(payload);
    expect(payload['network-name']).toBe('Saved Name');
    expect(payload.dmr.ipsc.port).toBe(50000);
    expect(wrapper.find('[data-test="menu-dirty-general"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="menu-dirty-ipsc"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="savebar-message"]').text()).toContain('Saved at');

    await go(router, '#ipsc');
    expect((wrapper.find('#setup-dmr-ipsc-port').element as HTMLInputElement).value).toBe('50010');
    wrapper.unmount();
  });

  it('shows the errors of the validation on the fields', async () => {
    mockAPI.post.mockResolvedValue({ data: { valid: false, errors: { dmr: { 'public-address': 'enter a host name' } } } });
    const { wrapper } = await mountAt('/admin/setup#dmr');
    await wrapper.find('#setup-dmr-public-address').setValue('https://dmr.example.org');
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="field-dmr.public-address"] [data-test="field-error"]').text()).toBe('enter a host name');
    expect(wrapper.find('[data-test="savebar-message"]').text()).toContain('not valid');
    wrapper.unmount();
  });

  it('puts the default back with Use the default', async () => {
    const { wrapper } = await mountAt('/admin/setup#general');
    const field = wrapper.find('[data-test="field-network-name"]');
    expect(field.find('[data-test="setting-default"]').text()).toContain('The default is DMRHub.');
    await field.find('[data-test="use-default"]').trigger('click');
    expect((wrapper.find('#setup-network-name').element as HTMLInputElement).value).toBe('DMRHub');
    expect(wrapper.find('[data-test="field-network-name"] [data-test="setting-default"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="menu-dirty-general"]').exists()).toBe(true);
    wrapper.unmount();
  });
});

describe('Admin Setup page locks and sources', () => {
  it('locks the settings of the deployment and names their source', async () => {
    const { wrapper } = await mountAt('/admin/setup#http');
    const port = wrapper.find('[data-test="field-http.port"]');
    expect(port.find('input').attributes('disabled')).toBeDefined();
    expect(port.find('[data-test="source-chip-lock"]').text()).toBe('Command line --http.port');
    expect(port.text()).toContain('The command line of the deployment sets this. Change it there and restart DMRHub.');

    const proxies = wrapper.find('[data-test="field-http.trusted-proxies"]');
    expect(proxies.find('textarea').attributes('disabled')).toBeDefined();
    expect(proxies.find('[data-test="source-chip-lock"]').text()).toBe('Config file');

    const site = wrapper.find('[data-test="field-http.canonical-host"]');
    expect(site.find('input').attributes('disabled')).toBeUndefined();
    expect(site.find('[data-test="source-chip-setup"]').text()).toBe('Saved in Setup');
    wrapper.unmount();
  });

  it('locks the settings that DMRHub reads at start', async () => {
    const { wrapper } = await mountAt('/admin/setup#http');
    const bind = wrapper.find('[data-test="field-http.bind"]');
    expect(bind.find('input').attributes('disabled')).toBeDefined();
    expect(bind.find('[data-test="source-chip-default"]').exists()).toBe(true);
    expect(bind.find('[data-test="source-chip-boot"]').text()).toBe('Read at start');
    expect(bind.text()).toContain('DMRHub reads this at start.');
    wrapper.unmount();
  });

  it('shows the environment variable of a secret and never its value', async () => {
    const { wrapper } = await mountAt('/admin/setup#general');
    const secret = wrapper.find('[data-test="field-secret"]');
    expect(secret.find('input').attributes('disabled')).toBeDefined();
    expect(secret.find('input').attributes('type')).toBe('password');
    expect((secret.find('input').element as HTMLInputElement).value).toBe('');
    expect(secret.find('[data-test="source-chip-lock"]').text()).toBe('Environment SECRET');
    expect(secret.text()).toContain('The environment of the deployment sets this.');
    wrapper.unmount();
  });

  it('says that a tab of start-only settings cannot change', async () => {
    const { wrapper } = await mountAt('/admin/setup#database');
    expect(wrapper.find('[data-test="section-note"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="save"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="savebar-message"]').text()).toBe('Nothing on this tab can change while DMRHub runs.');
    expect(wrapper.find('[data-test="menu-database"]').text()).toContain('Read at start');
    wrapper.unmount();
  });

  it('never shows a config key as a label', async () => {
    const { wrapper } = await mountAt('/admin/setup#dmr');
    expect(wrapper.find('section').text()).not.toMatch(/dmr\.(mmdvm|public-address|retention)/);
    wrapper.unmount();
  });
});
