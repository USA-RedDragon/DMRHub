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
    expect(wrapper.text()).not.toContain('/admin/setup#ipsc');
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

describe('Admin Setup page IP lists', () => {
  const ipLists = {
    mmdvm: { mode: 'block', block: ['203.0.113.7', '198.51.100.0/24'], allow: [] },
    webtransport: { mode: 'off', block: [], allow: [] },
    ipsc: { mode: 'allow', block: [], allow: [] },
    openbridge: { mode: 'off', block: [], allow: ['192.0.2.10'] },
  };
  const ipSettings = Object.fromEntries(
    tabPaths(TABS.find((t) => t.id === 'iplists')!).map((p) => [p, { ...plain, default: p.endsWith('.mode') ? 'off' : [] }]),
  );
  const withLists = () => {
    const cfg = config();
    (cfg.dmr as Record<string, unknown>)['ip-lists'] = JSON.parse(JSON.stringify(ipLists));
    getConfig(() => response(cfg, ipSettings));
  };
  const area = (w: VueWrapper, path: string) => w.find(`[data-test="field-${path}"]`);

  it('shows a card for each protocol with only the list of its mode', async () => {
    withLists();
    const { wrapper } = await mountAt('/admin/setup#iplists');
    const text = wrapper.find('[data-test="tab-iplists"]').text();
    for (const title of ['MMDVM hotspots and repeaters', 'Browser radios', 'Motorola IPSC repeaters', 'OpenBridge peers']) {
      expect(text).toContain(title);
    }
    expect(area(wrapper, 'dmr.ip-lists.mmdvm.block').exists()).toBe(true);
    expect(area(wrapper, 'dmr.ip-lists.mmdvm.allow').exists()).toBe(false);
    expect((wrapper.find('#setup-dmr-ip-lists-mmdvm-block').element as HTMLTextAreaElement).value).toBe('203.0.113.7\n198.51.100.0/24');
    expect(area(wrapper, 'dmr.ip-lists.mmdvm.block').find('[data-test="field-count"]').text()).toBe('2 entries');
    expect(area(wrapper, 'dmr.ip-lists.webtransport.block').exists()).toBe(false);
    expect(area(wrapper, 'dmr.ip-lists.webtransport.allow').exists()).toBe(false);
    expect(area(wrapper, 'dmr.ip-lists.ipsc.allow').find('[data-test="field-warning"]').text())
      .toBe('With no address listed, every IPSC repeater is refused.');
    expect(text).not.toMatch(/dmr\.ip-lists/);
    wrapper.unmount();
  });

  it('switches the shown list with the mode', async () => {
    withLists();
    const { wrapper } = await mountAt('/admin/setup#iplists');
    const radios = area(wrapper, 'dmr.ip-lists.openbridge.mode').findAll('input[type="radio"]');
    await radios[2]!.setValue(true);
    expect(area(wrapper, 'dmr.ip-lists.openbridge.allow').exists()).toBe(true);
    expect((wrapper.find('#setup-dmr-ip-lists-openbridge-allow').element as HTMLTextAreaElement).value).toBe('192.0.2.10');
    expect(wrapper.find('[data-test="menu-dirty-iplists"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('shows a bad entry at once and does not save it', async () => {
    withLists();
    const { wrapper } = await mountAt('/admin/setup#iplists');
    await wrapper.find('#setup-dmr-ip-lists-mmdvm-block').setValue('203.0.113.7\n10.1.2.3/8');
    expect(area(wrapper, 'dmr.ip-lists.mmdvm.block').find('[data-test="field-error"]').text())
      .toBe('10.1.2.3/8 has bits set after its prefix; use 10.0.0.0/8');
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).not.toHaveBeenCalled();
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="savebar-message"]').text()).toContain('not valid');
    wrapper.unmount();
  });

  it('saves the lists of the tab, an empty allow list among them', async () => {
    withLists();
    const { wrapper } = await mountAt('/admin/setup#iplists');
    await wrapper.find('#setup-dmr-ip-lists-mmdvm-block').setValue('203.0.113.7\n\n2001:db8::/32 ');
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    const payload = mockAPI.put.mock.calls[0]![1];
    expect(payload.dmr['ip-lists']).toEqual({
      ...ipLists,
      mmdvm: { mode: 'block', block: ['203.0.113.7', '2001:db8::/32'], allow: [] },
    });
    wrapper.unmount();
  });
});

describe('Admin Setup page routing tab', () => {
  const routingSettings = Object.fromEntries([
    ['dmr.routing.disable-dynamic-links', false], ['dmr.routing.dynamic-timeout', 9e11], ['dmr.routing.transmit', 'any'],
    ['dmr.routing.max-transmission', 0], ['dmr.routing.contention', 'mix'], ['dmr.routing.auto-create-talkgroups', false],
    ['dmr.routing.ephemeral-talkgroups', false], ['dmr.routing.ephemeral-ttl', 8.64e13], ['dmr.repeater-quota', 100],
  ].map(([p, d]) => [p, { ...plain, default: d }]));
  const routing = {
    'disable-dynamic-links': false, 'dynamic-timeout': 9e11, transmit: 'any', 'max-transmission': 0, contention: 'mix',
    'auto-create-talkgroups': false, 'ephemeral-talkgroups': false, 'ephemeral-ttl': 8.64e13,
  };
  const withRouting = () => {
    const cfg = config();
    (cfg.dmr as Record<string, unknown>).routing = { ...routing };
    (cfg.dmr as Record<string, unknown>)['repeater-quota'] = 100;
    getConfig(() => response(cfg, routingSettings));
  };
  const area = (w: VueWrapper, path: string) => w.find(`[data-test="field-${path}"]`);

  it('shows the defaults with only the fields that apply', async () => {
    withRouting();
    const { wrapper } = await mountAt('/admin/setup#routing');
    const text = wrapper.find('[data-test="tab-routing"]').text();
    for (const title of ['Dynamic talkgroups', 'Transmissions', 'Talkgroups', 'Repeaters']) {
      expect(text).toContain(title);
    }
    expect((wrapper.find('#setup-dmr-routing-disable-dynamic-links').attributes('data-state'))).toBe('checked');
    expect((wrapper.find('#setup-dmr-routing-dynamic-timeout').element as HTMLInputElement).value).toBe('15');
    expect(area(wrapper, 'dmr.routing.max-transmission').find('#setup-dmr-routing-max-transmission').exists()).toBe(false);
    expect(area(wrapper, 'dmr.routing.ephemeral-talkgroups').exists()).toBe(false);
    expect(area(wrapper, 'dmr.routing.ephemeral-ttl').exists()).toBe(false);
    expect(text).not.toMatch(/dmr\.routing|repeater-quota/);
    wrapper.unmount();
  });

  it('turns dynamic links off and hides the timeout', async () => {
    withRouting();
    const { wrapper } = await mountAt('/admin/setup#routing');
    await wrapper.find('#setup-dmr-routing-disable-dynamic-links').trigger('click');
    expect(area(wrapper, 'dmr.routing.dynamic-timeout').exists()).toBe(false);
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put.mock.calls[0]![1].dmr.routing['disable-dynamic-links']).toBe(true);
    wrapper.unmount();
  });

  it('limits the transmission length with its switch', async () => {
    withRouting();
    const { wrapper } = await mountAt('/admin/setup#routing');
    await wrapper.find('#setup-dmr-routing-max-transmission-toggle').trigger('click');
    expect((wrapper.find('#setup-dmr-routing-max-transmission').element as HTMLInputElement).value).toBe('180');
    await wrapper.find('#setup-dmr-routing-max-transmission').setValue('120');
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put.mock.calls[0]![1].dmr.routing['max-transmission']).toBe(120e9);
    wrapper.unmount();
  });

  it('shows ephemeral and its lifetime only when they apply', async () => {
    withRouting();
    const { wrapper } = await mountAt('/admin/setup#routing');
    await wrapper.find('#setup-dmr-routing-auto-create-talkgroups').trigger('click');
    expect(area(wrapper, 'dmr.routing.ephemeral-talkgroups').exists()).toBe(true);
    expect(area(wrapper, 'dmr.routing.ephemeral-ttl').exists()).toBe(false);
    await wrapper.find('#setup-dmr-routing-ephemeral-talkgroups').trigger('click');
    expect((wrapper.find('#setup-dmr-routing-ephemeral-ttl').element as HTMLInputElement).value).toBe('24');
    wrapper.unmount();
  });

  it('saves who may transmit, the contention and the quota', async () => {
    withRouting();
    const { wrapper } = await mountAt('/admin/setup#routing');
    await area(wrapper, 'dmr.routing.transmit').findAll('input[type="radio"]')[2]!.setValue(true);
    await area(wrapper, 'dmr.routing.contention').findAll('input[type="radio"]')[1]!.setValue(true);
    await wrapper.find('#setup-dmr-repeater-quota').setValue('5');
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    const payload = mockAPI.put.mock.calls[0]![1];
    expect(payload.dmr.routing.transmit).toBe('members');
    expect(payload.dmr.routing.contention).toBe('first');
    expect(payload.dmr['repeater-quota']).toBe(5);
    wrapper.unmount();
  });
});
