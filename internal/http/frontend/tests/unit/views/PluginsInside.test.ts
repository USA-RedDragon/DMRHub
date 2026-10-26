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
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';

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
import UploadPluginPage from '@/views/admin/UploadPluginPage.vue';
import PluginsSettings from '@/components/setup/DMR/PluginsSettings.vue';
import { formatIDs, parseIDs, runsLabel, status, stopNote, type AdminPlugin } from '@/services/plugins';
import { shortTime } from '@/lib/codec';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock; delete: Mock };

const ago = (minutes: number) => new Date(Date.now() - minutes * 60000).toISOString();

const base: AdminPlugin = {
  id: 1, name: 'Parrot', kind: 'builtin', version: 'v1.3.4', author: 'DMRHub', description: 'Plays each voice call back.',
  enabled: true, ranges: [{ first: 9990, last: 9990 }], hosts: [], online: true, connected_since: ago(60), last_seen_at: ago(0),
  address: '', token_created_at: '2026-10-23T12:00:00Z', calls_today: 42,
};
const locator: AdminPlugin = { ...base, id: 2, name: 'Location server', ranges: [{ first: 5050, last: 5059 }], calls_today: 7 };
const checkin: AdminPlugin = {
  ...base, id: 3, name: 'Net check-in', kind: 'uploaded', version: '0.4.1', author: 'N1CALL',
  description: 'Logs each radio that calls 9100 during a net and answers with a short tone.',
  ranges: [{ first: 9100, last: 9100 }], hosts: [{ address: 'nets.example.org:443', reason: 'Posts the check-in list when a net ends.', allowed: true }],
  calls_today: 12,
};
const weather: AdminPlugin = {
  ...base, id: 4, name: 'Weather', kind: 'external', version: '2.0.0', author: '', description: '', ranges: [{ first: 9200, last: 9200 }],
  online: false, connected_since: null, last_seen_at: ago(90), calls_today: 0,
};
const connection = { enabled: true, address: 'dmrhub.example.org:3006', fingerprint: '4F:2A:91', own_certificate: true };

function serve(plugins: AdminPlugin[] = [base, locator, checkin, weather]) {
  mockAPI.get.mockResolvedValue({ data: { plugins, connection, in_process: true } });
}

function choose(wrapper: VueWrapper, selector: string, name: string) {
  const input = wrapper.find(selector);
  const file = new File(['\0asm'], name, { type: 'application/wasm' });
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true });
  return input.trigger('change');
}

describe('plugins inside DMRHub, service', () => {
  it('reads and writes IDs as the mockup does', () => {
    expect(parseIDs('9990, 5050-5059')).toEqual([{ first: 9990, last: 9990 }, { first: 5050, last: 5059 }]);
    expect(parseIDs(' 9100 ')).toEqual([{ first: 9100, last: 9100 }]);
    expect(parseIDs('')).toEqual([]);
    expect(parseIDs('99a')).toBeNull();
    expect(parseIDs('5059-5050')).toBeNull();
    expect(formatIDs([{ first: 9990, last: 9990 }, { first: 5050, last: 5059 }])).toBe('9990, 5050-5059');
  });

  it('words where a plugin runs and its status', () => {
    expect(runsLabel('builtin')).toBe('Built in');
    expect(runsLabel('uploaded')).toBe('Uploaded');
    expect(runsLabel('external')).toBe('External');
    expect(status(base)).toBe('Running in DMRHub');
    expect(status({ ...checkin, online: false })).toBe('Starting');
    expect(status({ ...checkin, online: false, state: 'starting' })).toBe('Starting');
    expect(status({ ...checkin, enabled: false })).toBe('Off');
    expect(status({ ...checkin, state: 'off' })).toBe('Off');
  });

  it('words a plugin inside that stopped, waits or starts as the mockup does', () => {
    const now = Date.parse('2026-10-25T23:42:20Z');
    const stopped: AdminPlugin = {
      ...checkin, online: false, state: 'stopped', stopped_at: '2026-10-25T23:42:00Z', restart_at: '2026-10-25T23:42:50Z',
      stop_reason: 'It used more memory than a plugin may (64 MiB).', stops_last_hour: 4,
    };
    expect(status(stopped, now, 'America/Chicago')).toBe('Stopped at 18:42 CDT, starts again in 30 s');
    expect(status(stopped, Date.parse('2026-10-25T23:42:51Z'), 'America/Chicago')).toBe('Starting');
    expect(stopNote(stopped, now, 'America/Chicago')).toEqual({
      title: 'Stopped at 18:42 CDT.',
      body: 'It used more memory than a plugin may (64 MiB). DMRHub starts it again in 30 s. It has stopped 4 times in the last hour.',
    });
    expect(stopNote({ ...stopped, stops_last_hour: 1 }, now, 'America/Chicago')?.body).toContain('It has stopped once in the last hour.');
    expect(stopNote({ ...checkin, state: 'running' }, now)).toBeNull();
    expect(status({ ...locator, online: false, state: 'waiting', waiting_id: 5050, waiting_for: 'Old locator' })).toBe(
      'Waiting for its IDs, 5050 is used by Old locator',
    );
    expect(status({ ...base, state: 'running' })).toBe('Running in DMRHub');
  });
});

describe('PluginsPage with plugins inside DMRHub', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('lists where each plugin runs and its version', async () => {
    serve();
    const wrapper = mount(PluginsPage);
    await flushPromises();
    const headers = wrapper.findAll('th').map((h) => h.text());
    expect(headers).toEqual(['Name', 'Runs', 'Version', 'IDs', 'Status', 'Calls today', 'Actions']);
    const parrot = wrapper.find('[data-test="plugin-1"]').text();
    expect(parrot).toContain('Built in');
    expect(parrot).toContain('v1.3.4');
    expect(parrot).toContain('Running in DMRHub');
    expect(parrot).toContain('42');
    expect(wrapper.find('[data-test="plugin-2"]').text()).toContain('5050–5059');
    expect(wrapper.find('[data-test="plugin-3"]').text()).toContain('Uploaded');
    expect(wrapper.find('[data-test="plugin-4"]').text()).toContain('External');
    expect(wrapper.find('[data-test="plugin-4"]').text()).toContain('2.0.0');
    expect(wrapper.find('[data-test="upload-plugin"]').attributes('href')).toBe('/admin/plugins/upload');
    expect(wrapper.find('[data-test="upload-plugin"]').text()).toBe('Upload a plugin');
    expect(wrapper.find('[data-test="add-plugin"]').attributes('href')).toBe('/admin/plugins/new');
    expect(wrapper.find('[data-test="add-plugin"]').text()).toBe('Add an external plugin');
    expect(wrapper.find('[data-test="plugins-note"]').text()).toBe(
      'A plugin gets the calls to its IDs. It can send calls only from its IDs, to a radio or to a talkgroup among its IDs. ' +
        'Plugins that run inside DMRHub cannot reach your files, and reach only the hosts you allow on their page.',
    );
    expect(wrapper.text()).not.toMatch(/in-process|dmr\.plugins|cert-file/);
    wrapper.unmount();
  });
});

describe('PluginsPage with the states of plugins inside DMRHub', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('colors each state as the mockup does', async () => {
    serve([
      { ...base, state: 'running' },
      { ...checkin, online: false, state: 'starting' },
      { ...checkin, id: 5, name: 'Weather', online: false, state: 'stopped', stopped_at: ago(1), restart_at: new Date(Date.now() + 30000).toISOString() },
      { ...locator, online: false, state: 'waiting', waiting_id: 5050, waiting_for: 'Old locator' },
      { ...checkin, id: 6, name: 'Echo test', enabled: false, state: 'off' },
    ]);
    const wrapper = mount(PluginsPage);
    await flushPromises();
    const tone = (id: number) => wrapper.find(`[data-test="status-${id}"]`).attributes('data-tone');
    expect(tone(1)).toBe('ok');
    expect(tone(3)).toBe('info');
    expect(tone(5)).toBe('bad');
    expect(tone(2)).toBe('warn');
    expect(tone(6)).toBe('none');
    expect(wrapper.find('[data-test="plugin-3"]').text()).toContain('Starting');
    expect(wrapper.find('[data-test="plugin-5"]').text()).toMatch(/Stopped at \d\d:\d\d \S+, starts again in 30 s/);
    expect(wrapper.find('[data-test="plugin-2"]').text()).toContain('Waiting for its IDs, 5050 is used by Old locator');
    expect(wrapper.find('[data-test="plugin-6"]').text()).toContain('Off');
    expect(wrapper.text()).not.toContain('Not running');
    wrapper.unmount();
  });
});

describe('PluginsSettings with plugins inside DMRHub', () => {
  it('runs the plugins inside DMRHub unless turned off', async () => {
    const on = mount(PluginsSettings, { props: { modelValue: {} } });
    expect(on.text()).toContain('Plugins answer the calls to their IDs. Give them their IDs on the Plugins page under Admin.');
    expect(on.text()).toContain('Run plugins inside DMRHub');
    expect(on.find('[data-test="in-process-on"]').text()).toBe(
      'The parrot, the location server and the plugins you upload run as part of DMRHub, with nothing else to install.',
    );
    expect(on.find('[data-test="in-process-off"]').exists()).toBe(false);
    expect(on.text()).toContain('Accept plugins that run as their own programs');
    expect(on.find('[data-test="plugins-fields"]').exists()).toBe(false);
    await on.find('#plugins-in-process').trigger('click');
    expect(on.emitted('update:modelValue')!.at(-1)).toEqual([{ 'in-process': false }]);

    const off = mount(PluginsSettings, { props: { modelValue: { 'in-process': false } } });
    expect(off.find('[data-test="in-process-off"]').text()).toBe(
      'Turn this off only if you run every plugin as its own program, such as dmrhub-parrot and dmrhub-locator.',
    );
    expect(off.text()).not.toMatch(/in-process|cert-file|dmr\.plugins/);
  });
});

describe('UploadPluginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('refuses a file that is not a .wasm plugin', async () => {
    const wrapper = mount(UploadPluginPage);
    await choose(wrapper, '#up-file', 'plugin.exe');
    await flushPromises();
    expect(wrapper.find('[data-test="upload-error"]').text()).toBe(
      "This file is not a WebAssembly module. Choose the .wasm file that the plugin's build made.",
    );
    expect(wrapper.find('[data-test="upload-details"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="upload-go"]').attributes('disabled')).toBeDefined();
    expect(mockAPI.post).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it('shows what the plugin asks, then uploads and starts it', async () => {
    mockAPI.post.mockResolvedValueOnce({
      data: {
        name: 'Net check-in', version: '0.4.1', author: 'N1CALL', description: checkin.description,
        ranges: [{ first: 9100, last: 9100 }], hosts: [{ address: 'nets.example.org:443', reason: 'Posts the check-in list when a net ends.', allowed: false }],
      },
    });
    const wrapper = mount(UploadPluginPage);
    expect(wrapper.find('h2').text()).toBe('Upload a plugin');
    await choose(wrapper, '#up-file', 'net-checkin.wasm');
    await flushPromises();
    expect(mockAPI.post.mock.calls[0][0]).toBe('/admin/plugins/inspect');
    expect(mockAPI.post.mock.calls[0][1]).toBeInstanceOf(FormData);
    const details = wrapper.find('[data-test="upload-details"]').text();
    expect(details).toContain('Net check-in');
    expect(details).toContain('0.4.1');
    expect(details).toContain('N1CALL');
    expect(details).toContain('Logs each radio that calls 9100');
    expect(details).toContain('The plugin asks for these.');
    expect(details).toContain('The plugin asks to reach these hosts. It can reach only the ones you allow.');
    expect(details).toContain('nets.example.org:443');
    expect((wrapper.find('#up-ids').element as HTMLInputElement).value).toBe('9100');
    const host = wrapper.find('[data-test="host-0"]').element as HTMLInputElement;
    expect(host.checked).toBe(false);
    await wrapper.find('[data-test="host-0"]').setValue(true);

    mockAPI.post.mockResolvedValueOnce({ data: { ...checkin } });
    await wrapper.find('[data-test="upload-go"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post.mock.calls[1][0]).toBe('/admin/plugins/upload');
    const form = mockAPI.post.mock.calls[1][1] as FormData;
    expect(form.get('ranges')).toBe('[{"first":9100,"last":9100}]');
    expect(form.get('allowed_hosts')).toBe('["nets.example.org:443"]');
    expect(form.get('file')).toBeInstanceOf(File);
    expect(wrapper.find('[data-test="upload-done"]').text()).toBe('Net check-in is running.');
    wrapper.unmount();
  });
  it('shows each problem of a file under its field', async () => {
    const manifest = { name: 'Parrot two', version: '0.1.0', author: '', description: '', ranges: [{ first: 9990, last: 9990 }], hosts: [] };
    mockAPI.post.mockRejectedValueOnce({
      response: {
        status: 400,
        data: {
          error: '9990 is already used by Parrot. Change the IDs, or turn Parrot off first.',
          errors: [
            { field: 'ids', message: '9990 is already used by Parrot. Change the IDs, or turn Parrot off first.' },
            { field: 'hosts', message: 'The plugin asks to reach “example”, which is not a host and port such as nets.example.org:443.' },
          ],
          manifest,
        },
      },
    });
    const wrapper = mount(UploadPluginPage);
    await choose(wrapper, '#up-file', 'parrot2.wasm');
    await flushPromises();
    expect(wrapper.find('[data-test="upload-details"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="upload-ids-error"]').text()).toBe('9990 is already used by Parrot. Change the IDs, or turn Parrot off first.');
    expect(wrapper.find('[data-test="upload-hosts-error"]').text()).toBe(
      'The plugin asks to reach “example”, which is not a host and port such as nets.example.org:443.',
    );
    expect(wrapper.find('[data-test="upload-error"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="upload-go"]').attributes('disabled')).toBeDefined();
    wrapper.unmount();
  });

  it('shows a problem of the file itself under the file', async () => {
    mockAPI.post.mockRejectedValueOnce({
      response: {
        status: 413,
        data: { error: 'This file is 41 MiB. A plugin can be at most 32 MiB.', errors: [{ field: 'file', message: 'This file is 41 MiB. A plugin can be at most 32 MiB.' }] },
      },
    });
    const wrapper = mount(UploadPluginPage);
    await choose(wrapper, '#up-file', 'big.wasm');
    await flushPromises();
    expect(wrapper.findAll('[data-test="upload-error"]').map((e) => e.text())).toEqual(['This file is 41 MiB. A plugin can be at most 32 MiB.']);
    expect(wrapper.find('[data-test="upload-details"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('shows the problems of an upload that the hub refuses under their fields', async () => {
    mockAPI.post.mockResolvedValueOnce({ data: { name: 'Net check-in', version: '0.4.1', author: '', description: '', ranges: [], hosts: [] } });
    const wrapper = mount(UploadPluginPage);
    await choose(wrapper, '#up-file', 'net-checkin.wasm');
    await flushPromises();
    await wrapper.find('#up-ids').setValue('0');
    mockAPI.post.mockRejectedValueOnce({
      response: {
        status: 400,
        data: {
          error: 'x',
          errors: [
            { field: 'file', message: 'Net check-in is already uploaded. Open it and choose “Upload a new version”.' },
            { field: 'ids', message: 'The plugin asks for ID 0, which is not a radio or talkgroup ID.' },
          ],
        },
      },
    });
    await wrapper.find('[data-test="upload-go"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="upload-error"]').text()).toBe('Net check-in is already uploaded. Open it and choose “Upload a new version”.');
    expect(wrapper.find('[data-test="upload-ids-error"]').text()).toBe('The plugin asks for ID 0, which is not a radio or talkgroup ID.');
    expect(wrapper.find('[data-test="upload-save-error"]').exists()).toBe(false);
    wrapper.unmount();
  });
});

describe('PluginPage of a plugin inside DMRHub', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    nav.params = { id: '3' };
  });

  it('edits an uploaded plugin as the mockup draws it', async () => {
    serve();
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    expect(wrapper.find('h2').text()).toContain('Net check-in');
    expect(wrapper.find('h2').text()).toContain('Uploaded');
    expect(wrapper.find('[data-test="inside-desc"]').text()).toBe(
      'Logs each radio that calls 9100 during a net and answers with a short tone. Version 0.4.1.',
    );
    expect((wrapper.find('#pp-ids').element as HTMLInputElement).value).toBe('9100');
    expect((wrapper.find('[data-test="host-0"]').element as HTMLInputElement).checked).toBe(true);
    expect(wrapper.text()).toContain('The hosts this plugin asks to reach. It can reach only the ones you allow.');
    expect(wrapper.text()).toContain('Hosts that the new version adds start out not allowed.');
    expect(wrapper.find('#pp-file').exists()).toBe(true);
    expect(wrapper.find('label[for="pp-file"]').text()).toBe('Upload a new version');
    expect(wrapper.find('[data-test="token-dialog"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('Make a new token');

    await wrapper.find('#pp-ids').setValue('91 00');
    await wrapper.find('[data-test="inside-save"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="inside-error"]').text()).toBe('Use IDs and ranges such as 9990, 5050-5059.');
    expect(mockAPI.put).not.toHaveBeenCalled();

    await wrapper.find('#pp-ids').setValue('9100, 9110-9119');
    await wrapper.find('[data-test="host-0"]').setValue(false);
    mockAPI.put.mockResolvedValueOnce({ data: { ...checkin, ranges: [{ first: 9100, last: 9100 }, { first: 9110, last: 9119 }] } });
    await wrapper.find('[data-test="inside-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenCalledWith('/admin/plugins/3', {
      name: 'Net check-in', enabled: true, ranges: [{ first: 9100, last: 9100 }, { first: 9110, last: 9119 }], allowed_hosts: [],
    });
    expect(wrapper.find('[data-test="inside-saved"]').text()).toBe('Saved.');
    wrapper.unmount();
  });

  it('uploads a new version when saving', async () => {
    serve();
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    await choose(wrapper, '#pp-file', 'net-checkin-0.5.wasm');
    mockAPI.post.mockResolvedValueOnce({ data: { ...checkin, version: '0.5.0' } });
    mockAPI.put.mockResolvedValueOnce({ data: { ...checkin, version: '0.5.0' } });
    await wrapper.find('[data-test="inside-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post.mock.calls[0][0]).toBe('/admin/plugins/3/module');
    expect(mockAPI.post.mock.calls[0][1]).toBeInstanceOf(FormData);
    expect(mockAPI.put).toHaveBeenCalled();
    expect(wrapper.find('[data-test="inside-desc"]').text()).toContain('Version 0.5.0.');
    wrapper.unmount();
  });

  it('removes an uploaded plugin after asking on the page', async () => {
    serve();
    mockAPI.delete.mockResolvedValue({});
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    await wrapper.find('[data-test="inside-remove"]').trigger('click');
    expect(wrapper.find('[data-test="inside-confirm"]').text()).toContain('Remove Net check-in? Calls to 9100 will go nowhere.');
    await wrapper.find('[data-test="inside-keep"]').trigger('click');
    expect(wrapper.find('[data-test="inside-confirm"]').exists()).toBe(false);
    expect(mockAPI.delete).not.toHaveBeenCalled();
    await wrapper.find('[data-test="inside-remove"]').trigger('click');
    await wrapper.find('[data-test="inside-yes"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/admin/plugins/3');
    expect(nav.push).toHaveBeenCalledWith('/admin/plugins');
    wrapper.unmount();
  });

  it('says why a plugin stopped and when it starts again', async () => {
    const stoppedAt = new Date(Date.now() - 5000);
    const stopped: AdminPlugin = {
      ...checkin, online: false, state: 'stopped', stopped_at: stoppedAt.toISOString(),
      restart_at: new Date(Date.now() + 29500).toISOString(), stop_reason: 'It did not answer DMRHub for 30 s.', stops_last_hour: 2,
    };
    serve([base, locator, stopped, weather]);
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    const box = wrapper.find('[data-test="inside-stopped"]');
    expect(box.attributes('role')).toBe('alert');
    expect(box.text()).toBe(
      `Stopped at ${shortTime(stoppedAt)}. It did not answer DMRHub for 30 s. DMRHub starts it again in 30 s. It has stopped 2 times in the last hour.`,
    );
    wrapper.unmount();
  });

  it('shows no stop box for a plugin that runs', async () => {
    serve();
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    expect(wrapper.find('[data-test="inside-stopped"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('shows a built-in plugin without Upload a new version and Remove plugin', async () => {
    serve();
    nav.params = { id: '1' };
    const wrapper = mount(PluginPage, { attachTo: document.body });
    await flushPromises();
    expect(wrapper.find('h2').text()).toContain('Parrot');
    expect(wrapper.find('h2').text()).toContain('Built in');
    expect(wrapper.find('[data-test="inside-desc"]').text()).toBe('Plays each voice call back. Version v1.3.4.');
    expect(wrapper.find('#pp-file').exists()).toBe(false);
    expect(wrapper.find('[data-test="inside-remove"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('Network');
    wrapper.unmount();
  });
});
