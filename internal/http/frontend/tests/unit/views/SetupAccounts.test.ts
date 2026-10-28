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
import { createMemoryHistory, createRouter } from 'vue-router';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import SetupPage from '@/views/admin/SetupPage.vue';
import { DAY, HOUR, tabPaths, TABS } from '@/lib/setupMenu';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const plain = { editable: true, locked: false, start_only: false, source: 'default', not_in_effect: false, disagree: false };
const checks = { 'user-id': true, callsign: true, 'radio-ids': true, 'repeater-ids': true, 'hotspot-ids': true };

const config = (over: { disable?: boolean; length?: number } = {}) => ({
  'network-name': 'Example DMR Network',
  registration: 'open',
  'login-backoff': true,
  visibility: 'public',
  http: { 'canonical-host': 'https://dmrhub.example.org', 'session-length': over.length ?? 30 * DAY },
  database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
  dmr: { 'disable-radio-id-validation': over.disable ?? false, 'radio-id-checks': { ...checks }, transcoding: { 'ambe-servers': [] } },
});

const response = (cfg = config()) => ({
  data: {
    ...cfg,
    secrets: { secretSet: true, passwordSaltSet: true, smtpPasswordSet: false },
    revision: 41,
    disagree: false,
    settings: Object.fromEntries(tabPaths(TABS.find((t) => t.id === 'accounts')!).map((p) => [p, { ...plain }])),
    replicas: [],
  },
});

const mountAccounts = async (cfg = config()): Promise<VueWrapper> => {
  mockAPI.get.mockImplementation((url: string) => (url === '/config/transcoding' ? Promise.resolve({ data: { servers: [] } }) : Promise.resolve(response(cfg))));
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/setup', component: SetupPage }] });
  await router.push('/admin/setup#accounts');
  await router.isReady();
  const wrapper = mount(SetupPage, { global: { plugins: [router], mocks: { $toast: { add: vi.fn() } } }, attachTo: document.body });
  await flushPromises();
  return wrapper;
};

beforeEach(() => {
  vi.clearAllMocks();
  window.scrollTo = vi.fn() as unknown as typeof window.scrollTo;
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  mockAPI.put.mockResolvedValue({ data: { revision: 42, replicas: [], applyErrors: [] } });
});

describe('Setup Accounts tab', () => {
  it('shows the four cards with labels only, under Users', async () => {
    const wrapper = await mountAccounts();
    const tab = wrapper.find('[data-test="tab-accounts"]');
    const text = tab.text();
    for (const t of ['Registration', 'RadioID checks', 'Logins', 'Visibility', 'Anyone', 'Invite only', 'Nobody',
      'Turn off all checks', 'DMR ID has 7 digits', 'Callsign matches DMR ID', 'Added radio IDs', 'Repeater IDs',
      'Hotspot IDs start with owner ID', 'Pause an account after 10 failed logins', 'Login lasts',
      'Everything', 'No names or positions', 'Nothing']) {
      expect(text).toContain(t);
    }
    expect(text).not.toMatch(/radio-id-checks|session-length|login-backoff|registration\b|visibility\b/);
    expect(tab.findAll('p.text-muted-foreground')).toHaveLength(0);
    expect(wrapper.text()).not.toContain('go into effect at once');
    expect(wrapper.find('[data-test="menu-accounts"]').exists()).toBe(true);
    expect((wrapper.find('#setup-http-session-length').element as HTMLInputElement).value).toBe('30');
    expect((wrapper.find('[data-test="duration-unit"]').element as HTMLSelectElement).value).toBe(String(DAY));
    wrapper.unmount();
  });

  it('hides the five checks while the old switch turns them all off', async () => {
    const wrapper = await mountAccounts(config({ disable: true }));
    expect(wrapper.find('[data-test="field-dmr.radio-id-checks.callsign"]').exists()).toBe(false);
    await wrapper.find('#setup-dmr-disable-radio-id-validation').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="field-dmr.radio-id-checks.callsign"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('saves the mode, the checks and the session length', async () => {
    const wrapper = await mountAccounts(config({ length: 12 * HOUR }));
    expect((wrapper.find('[data-test="duration-unit"]').element as HTMLSelectElement).value).toBe(String(HOUR));
    const invite = wrapper.findAll('[data-test="field-registration"] input[type="radio"]')[1]!;
    await invite.setValue(true);
    await wrapper.find('#setup-dmr-radio-id-checks-hotspot-ids').trigger('click');
    await wrapper.find('#setup-http-session-length').setValue('2');
    await wrapper.find('[data-test="duration-unit"]').setValue(String(DAY));
    const hide = wrapper.findAll('[data-test="field-visibility"] input[type="radio"]')[1]!;
    await hide.setValue(true);
    await wrapper.find('[data-test="save"]').trigger('click');
    await flushPromises();
    const payload = mockAPI.put.mock.calls[0]![1];
    expect(payload.registration).toBe('invite');
    expect(payload.visibility).toBe('hide-names');
    expect(payload.dmr['radio-id-checks']['hotspot-ids']).toBe(false);
    expect(payload.dmr['radio-id-checks'].callsign).toBe(true);
    expect(payload.http['session-length']).toBe(2 * DAY);
    wrapper.unmount();
  });
});

describe('Setup DMR tab', () => {
  it('no longer has the RadioID switch', async () => {
    expect(tabPaths(TABS.find((t) => t.id === 'dmr')!)).not.toContain('dmr.disable-radio-id-validation');
    expect(tabPaths(TABS.find((t) => t.id === 'accounts')!)).toContain('dmr.disable-radio-id-validation');
  });
});
