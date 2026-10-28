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
    delete: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

const nav = vi.hoisted(() => ({ params: { id: '1' } as Record<string, string>, push: vi.fn(), replace: vi.fn() }));

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: nav.params }),
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
}));

import API from '@/services/API';
import PluginsPage from '@/views/admin/PluginsPage.vue';
import PluginPage from '@/views/admin/PluginPage.vue';
import PluginsSettings from '@/components/setup/DMR/PluginsSettings.vue';
import { formatRanges, formatSpan, status, type AdminPlugin } from '@/services/plugins';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock; delete: Mock };

const ago = (minutes: number) => new Date(Date.now() - minutes * 60000).toISOString();

const parrot: AdminPlugin = {
  id: 1, name: 'Parrot', kind: 'external', version: '', author: '', description: '', hosts: [],
  enabled: true, ranges: [{ first: 9990, last: 9990 }], online: true,
  connected_since: ago(120), last_seen_at: ago(0), address: '10.0.4.7:51234', token_created_at: '2026-10-23T06:00:00Z', calls_today: 12,
};
const locator: AdminPlugin = {
  ...parrot, id: 2, name: 'Location server', ranges: [{ first: 5050, last: 5059 }], online: false, connected_since: null,
  last_seen_at: '2026-10-21T12:00:00Z', address: '', calls_today: 0,
};
const weather: AdminPlugin = {
  ...parrot, id: 3, name: 'Weather net', enabled: false, ranges: [{ first: 9998, last: 9998 }, { first: 310001, last: 310009 }],
  online: false, connected_since: null, last_seen_at: null, calls_today: 0,
};
const connection = { enabled: true, address: 'dmrhub.example.org:3006', fingerprint: '4F:2A:91', own_certificate: true };

function serve(plugins: AdminPlugin[] = [parrot, locator, weather]) {
  mockAPI.get.mockResolvedValue({ data: { plugins, connection } });
}

describe('plugins service', () => {
  it('words ranges, spans and status as the mockup does', () => {
    expect(formatRanges([{ first: 9990, last: 9990 }])).toBe('9990');
    expect(formatRanges([{ first: 310001, last: 310009 }, { first: 9998, last: 9998 }])).toBe('310001–310009, 9998');
    expect(formatRanges([])).toBe('None');
    const now = Date.parse('2026-10-24T12:00:00Z');
    expect(formatSpan('2026-10-24T10:00:00Z', now)).toBe('2 hours');
    expect(formatSpan('2026-10-21T12:00:00Z', now)).toBe('3 days');
    expect(formatSpan('2026-10-24T11:55:00Z', now)).toBe('5 minutes');
    expect(status(parrot)).toBe('Connected for 2 hours, from 10.0.4.7');
    expect(status(locator, now, 'America/Chicago')).toBe('Not connected since 21 October 2026');
    expect(status({ ...locator, last_seen_at: '2026-10-24T09:12:00Z' }, now, 'America/Chicago')).toBe('Not connected since 04:12 CDT');
    expect(status({ ...locator, last_seen_at: '2026-10-23T12:00:01Z' }, now, 'America/Chicago')).toBe('Not connected since 07:00 CDT');
    expect(status(weather)).toBe('Off');
    expect(status({ ...weather, enabled: true })).toBe('Not connected yet');
  });
});

describe('PluginsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('lists the plugins with their IDs, status and calls', async () => {
    serve();
    const wrapper = mount(PluginsPage);
    await flushPromises();
    const row = wrapper.find('[data-test="plugin-1"]').text();
    expect(row).toContain('Parrot');
    expect(row).toContain('9990');
    expect(row).toContain('Connected for 2 hours, from 10.0.4.7');
    expect(row).toContain('12');
    expect(wrapper.find('[data-test="plugin-3"]').text()).toContain('310001–310009');
    expect(wrapper.find('[data-test="plugin-3"]').text()).toContain('Off');
    expect(wrapper.find('[data-test="add-plugin"]').attributes('href')).toBe('/admin/plugins/new');
    expect(wrapper.text()).not.toMatch(/dmr\.plugins|cert-file/);
    wrapper.unmount();
  });

  it('says when there are no plugins', async () => {
    mockAPI.get.mockResolvedValue({ data: { plugins: [], connection: { ...connection, enabled: false } } });
    const wrapper = mount(PluginsPage);
    await flushPromises();
    expect(wrapper.find('[data-test="no-plugins"]').exists()).toBe(true);
    wrapper.unmount();
  });
});

describe('PluginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    nav.params = { id: '1' };
  });

  it('adds a plugin and shows its token once', async () => {
    serve([]);
    nav.params = { id: 'new' };
    mockAPI.post.mockResolvedValue({ data: { plugin: { ...parrot, online: false }, token: 'dmrhub_plg_abc' } });
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    expect(wrapper.find('h1').text()).toBe('Add a plugin');
    await wrapper.find('#plugin-name').setValue('Parrot');
    await wrapper.find('#from-0').setValue('9990');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/admin/plugins', { name: 'Parrot', ranges: [{ first: 9990, last: 9990 }] });
    const dialog = wrapper.find('[data-test="token-dialog"]');
    expect(dialog.exists()).toBe(true);
    expect((dialog.find('#plugin-token').element as HTMLInputElement).value).toBe('dmrhub_plg_abc');
    expect(dialog.text()).toContain('dmrhub.example.org:3006');
    expect(dialog.text()).toContain('SHA-256 4F:2A:91');
    await wrapper.find('[data-test="token-done"]').trigger('click');
    await flushPromises();
    expect(nav.replace).toHaveBeenCalledWith('/admin/plugins/1');
    expect(wrapper.find('[data-test="token-dialog"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('saves a plugin and shows what the hub refuses', async () => {
    serve();
    mockAPI.put.mockResolvedValueOnce({ data: { ...parrot, name: 'Echo', ranges: [{ first: 9990, last: 9991 }] } });
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    expect(wrapper.find('h1').text()).toBe('Parrot');
    expect(wrapper.find('[data-test="plugin-subtitle"]').text()).toBe('Connected for 2 hours, from 10.0.4.7 · 12 calls today');
    expect(wrapper.find('[data-test="connection"]').text()).toContain('Made on 23 October 2026');
    await wrapper.find('#plugin-name').setValue('Echo');
    await wrapper.find('#to-0').setValue('9991');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/admin/plugins/1', { name: 'Echo', enabled: true, ranges: [{ first: 9990, last: 9991 }] });
    expect(wrapper.find('[data-test="save-message"]').text()).toBe('Saved.');

    mockAPI.put.mockRejectedValueOnce({ response: { status: 400, data: { error: 'Pick IDs from 1 to 16777215' } } });
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="save-message"]').text()).toBe('Pick IDs from 1 to 16777215');

    await wrapper.find('#from-0').setValue('abc');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="save-message"]').text()).toBe('IDs are whole numbers.');
    wrapper.unmount();
  });

  it('makes a new token and deletes after asking', async () => {
    serve();
    mockAPI.post.mockResolvedValue({ data: { plugin: parrot, token: 'dmrhub_plg_new' } });
    mockAPI.delete.mockResolvedValue({});
    const confirm = vi.spyOn(window, 'confirm');
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    await wrapper.find('[data-test="new-token"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/admin/plugins/1/token');
    expect((wrapper.find('#plugin-token').element as HTMLInputElement).value).toBe('dmrhub_plg_new');
    await wrapper.find('[data-test="token-done"]').trigger('click');
    await flushPromises();
    expect(nav.replace).not.toHaveBeenCalled();

    confirm.mockReturnValueOnce(false);
    await wrapper.find('[data-test="delete"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).not.toHaveBeenCalled();
    confirm.mockReturnValueOnce(true);
    await wrapper.find('[data-test="delete"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/admin/plugins/1');
    expect(nav.push).toHaveBeenCalledWith('/admin/plugins');
    wrapper.unmount();
  });

  it('says when there is no such plugin', async () => {
    serve();
    nav.params = { id: '99' };
    const wrapper = mount(PluginPage);
    await flushPromises();
    expect(wrapper.find('[data-test="plugin-missing"]').exists()).toBe(true);
    wrapper.unmount();
  });
});

describe('PluginsSettings', () => {
  it('shows the fields only when plugins are accepted, with no config keys', async () => {
    const off = mount(PluginsSettings, { props: { modelValue: {} } });
    expect(off.find('[data-test="plugins-fields"]').exists()).toBe(false);
    expect(off.text()).toContain('Accept plugins');
    const on = mount(PluginsSettings, { props: { modelValue: { enabled: true } } });
    expect((on.find('#plugins-port').element as HTMLInputElement).value).toBe('3006');
    expect(on.find('[data-test="plugins-own-cert"]').exists()).toBe(true);
    expect(on.text()).not.toMatch(/cert-file|key-file|dmr\.plugins/);
    await on.find('#plugins-port').setValue('3007');
    expect(on.emitted('update:modelValue')!.at(-1)).toEqual([{ enabled: true, port: 3007 }]);
    const withErrors = mount(PluginsSettings, { props: { modelValue: { enabled: true }, errors: { port: 'bad port', 'key-file': 'set both' } } });
    expect(withErrors.text()).toContain('bad port');
    expect(withErrors.text()).toContain('set both');
  });
});
