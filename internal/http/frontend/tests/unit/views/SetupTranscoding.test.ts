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

import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import TranscodingSettings from '@/components/setup/DMR/TranscodingSettings.vue';
import SetupWizard from '@/views/SetupWizard.vue';
import type { ServerStatus, TranscodingStatus } from '@/lib/codec';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

type Server = { address: string; calls: number };

const HOST_PORT = 'Enter a host and port for each server, such as ambeserver:2460.';
const CALLS = 'Calls at once is a whole number of 1 or more, or empty for no limit.';
const HELP = 'Host and UDP port of each server. Calls at once: leave it empty when the server has no limit; use 1 for an '
  + 'AMBEserver with one DVSI chip. Each converted call uses one server for its whole length. When every server is '
  + 'busy, a call passes through in its own codec. With no server, transcoding is off.';

const two: Server[] = [
  { address: 'ambeserver:2460', calls: 0 },
  { address: 'thumbdv.example.org:2460', calls: 1 },
];

const status = (server: string, answered: number | null, calls = 0, limit = 0): ServerStatus => ({
  server,
  calls_limit: limit,
  answered_seconds: answered,
  calls,
});

const config = (servers: Server[]) => ({
  data: {
    dmr: {
      'radio-id-url': 'https://radioid.net/static/users.json',
      mmdvm: { port: 62031 },
      transcoding: { 'ambe-servers': servers },
    },
    http: {}, database: {}, smtp: {}, redis: {}, metrics: {}, pprof: {},
  },
});

const serve = (servers: Server[], answer: () => TranscodingStatus) => {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/config/transcoding') {
      return Promise.resolve({ data: answer() });
    }
    return Promise.resolve(config(servers));
  });
};

const accept = () => {
  mockAPI.post.mockResolvedValue({ data: { valid: true, errors: {} } });
  mockAPI.put.mockResolvedValue({ data: {} });
};

const statusCalls = () => mockAPI.get.mock.calls.filter((c) => c[0] === '/config/transcoding').length;

const mountWizard = (path: string) => mount(SetupWizard, {
  attachTo: document.body,
  global: {
    mocks: { $route: { path, query: {} }, $router: { push: vi.fn() }, $toast: { add: vi.fn() } },
  },
});

const sent = (mock: Mock) => {
  const payload = mock.mock.calls[0]![1] as { dmr: { transcoding: Record<string, unknown> } };
  return payload.dmr.transcoding['ambe-servers'];
};

const statusCell = (wrapper: VueWrapper, i: number) => wrapper.find(`[data-test="ambe-server-status-${i}"]`);

describe('Transcoding settings', { timeout: 20000 }, () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the table, its help and the add button, and no config keys', () => {
    const wrapper = mount(TranscodingSettings, { props: { modelValue: { 'ambe-servers': two }, showStatus: true } });
    const text = wrapper.text();
    expect(text).toContain('Transcoding');
    expect(text).toContain('Repeaters and hotspots that send Codec2 can talk with AMBE radios.');
    expect(wrapper.findAll('th').map((th) => th.text())).toEqual(['AMBE server', 'Calls at once', 'Status', 'Actions']);
    expect(wrapper.find('th .sr-only').text()).toBe('Actions');
    expect(wrapper.find('[data-test="ambe-server-add"]').text()).toBe('Add AMBE server');
    expect(wrapper.find('[data-test="ambe-server-remove-0"]').text()).toBe('Remove');
    expect(text).toContain(HELP);
    expect(text).not.toMatch(/dmr\.|transcoding\.|ambe-servers|ambe-server\b/);
    expect(text).not.toContain('NEW');
  });

  it('shows each server with its calls at once, and no limit as empty', () => {
    const wrapper = mount(TranscodingSettings, { props: { modelValue: { 'ambe-servers': two } } });
    const address = wrapper.find('[data-test="ambe-server-address-0"]');
    const calls0 = wrapper.find('[data-test="ambe-server-calls-0"]');
    expect((address.element as HTMLInputElement).value).toBe('ambeserver:2460');
    expect(address.attributes('aria-label')).toBe('AMBE server');
    expect(address.attributes('placeholder')).toBe('ambeserver.example.org:2460');
    expect((calls0.element as HTMLInputElement).value).toBe('');
    expect(calls0.attributes('aria-label')).toBe('Calls at once');
    expect(calls0.attributes('placeholder')).toBe('No limit');
    expect(calls0.attributes('inputmode')).toBe('numeric');
    expect((wrapper.find('[data-test="ambe-server-address-1"]').element as HTMLInputElement).value)
      .toBe('thumbdv.example.org:2460');
    expect((wrapper.find('[data-test="ambe-server-calls-1"]').element as HTMLInputElement).value).toBe('1');
  });

  it('shows only the header and the add button with no server', () => {
    const wrapper = mount(TranscodingSettings, { props: { modelValue: {} } });
    expect(wrapper.findAll('tbody tr')).toHaveLength(0);
    expect(wrapper.find('[data-test="ambe-server-add"]').exists()).toBe(true);
  });

  it('adds, edits and removes servers', async () => {
    const wrapper = mount(TranscodingSettings, {
      props: {
        modelValue: { 'ambe-servers': two },
        'onUpdate:modelValue': (v: Record<string, unknown>) => wrapper.setProps({ modelValue: v }),
      },
    });
    await wrapper.find('[data-test="ambe-server-add"]').trigger('click');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([{ 'ambe-servers': [...two, { address: '', calls: 0 }] }]);
    await wrapper.find('[data-test="ambe-server-address-2"]').setValue('[::1]:2460');
    await wrapper.find('[data-test="ambe-server-calls-2"]').setValue('4');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([
      { 'ambe-servers': [...two, { address: '[::1]:2460', calls: '4' }] },
    ]);
    await wrapper.find('[data-test="ambe-server-remove-0"]').trigger('click');
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([
      { 'ambe-servers': [two[1], { address: '[::1]:2460', calls: '4' }] },
    ]);
    expect(wrapper.findAll('tbody tr')).toHaveLength(2);
  });

  it('shows the error of the servers', () => {
    const wrapper = mount(TranscodingSettings, {
      props: { modelValue: { 'ambe-servers': [{ address: 'x', calls: 0 }] }, errors: { 'ambe-servers': HOST_PORT } },
    });
    expect(wrapper.text()).toContain(HOST_PORT);
    expect(wrapper.find('[data-test="ambe-server-address-0"]').attributes('aria-invalid')).toBe('true');
    expect(wrapper.find('[data-test="ambe-server-calls-0"]').attributes('aria-invalid')).toBeUndefined();
  });

  it('does not ask for the status on the first setup, and has no Status column', async () => {
    serve(two, () => ({ servers: [status('ambeserver:2460', 2)] }));
    const wrapper = mountWizard('/setup');
    await flushPromises();
    expect(statusCalls()).toBe(0);
    expect(wrapper.find('[data-test="ambe-servers"]').findAll('th').map((th) => th.text()))
      .toEqual(['AMBE server', 'Calls at once', 'Actions']);
    expect(statusCell(wrapper, 0).exists()).toBe(false);
  });

  it('saves the servers under dmr transcoding, with an empty calls at once as 0', async () => {
    serve(two, () => ({ servers: [] }));
    accept();
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    await wrapper.find('[data-test="ambe-server-add"]').trigger('click');
    await wrapper.find('[data-test="ambe-server-address-2"]').setValue(' [::1]:2460 ');
    await wrapper.find('[data-test="ambe-server-calls-2"]').setValue('4');
    await wrapper.find('[data-test="ambe-server-calls-1"]').setValue('');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const want = [
      { address: 'ambeserver:2460', calls: 0 },
      { address: 'thumbdv.example.org:2460', calls: 0 },
      { address: '[::1]:2460', calls: 4 },
    ];
    expect(sent(mockAPI.post)).toEqual(want);
    expect(sent(mockAPI.put)).toEqual(want);
  });

  it('saves no server as an empty list', async () => {
    serve(two, () => ({ servers: [] }));
    accept();
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    await wrapper.find('[data-test="ambe-server-remove-0"]').trigger('click');
    await wrapper.find('[data-test="ambe-server-remove-0"]').trigger('click');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(sent(mockAPI.put)).toEqual([]);
  });

  for (const [what, address, calls, error] of [
    ['an empty address', '', '', HOST_PORT],
    ['an address that is not a host and port', 'ambeserver', '', HOST_PORT],
    ['port 0', 'ambeserver:0', '', HOST_PORT],
    ['calls at once of 0', 'ambeserver:2460', '0', CALLS],
    ['calls at once that is not a whole number', 'ambeserver:2460', '1.5', CALLS],
  ] as const) {
    it(`does not save ${what}`, async () => {
      serve(two, () => ({ servers: [] }));
      const wrapper = mountWizard('/admin/setup');
      await flushPromises();
      await wrapper.find('[data-test="ambe-server-add"]').trigger('click');
      await wrapper.find('[data-test="ambe-server-address-2"]').setValue(address);
      await wrapper.find('[data-test="ambe-server-calls-2"]').setValue(calls);
      await wrapper.find('form').trigger('submit');
      await flushPromises();
      expect(mockAPI.post).not.toHaveBeenCalled();
      expect(mockAPI.put).not.toHaveBeenCalled();
      expect(wrapper.find('[data-test="transcoding-card"] .p-error').text()).toBe(error);
    });
  }

  it('shows the error from the server', async () => {
    serve(two, () => ({ servers: [] }));
    mockAPI.post.mockResolvedValue({
      data: { valid: false, errors: { dmr: { transcoding: { 'ambe-servers': 'the AMBE servers must be different' } } } },
    });
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.put).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="transcoding-card"] .p-error').text()).toBe('the AMBE servers must be different');
  });

  it('shows the status of each server by its address', async () => {
    serve(two, () => ({
      servers: [status('thumbdv.example.org:2460', null, 0, 1), status('ambeserver:2460', 2, 3)],
    }));
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    expect(statusCell(wrapper, 0).text()).toBe('Answered 2 seconds ago. 3 calls now.');
    expect(statusCell(wrapper, 0).find('.bg-green-500').exists()).toBe(true);
    expect(statusCell(wrapper, 1).text()).toBe('The AMBE server did not answer.');
    expect(statusCell(wrapper, 1).find('.bg-red-500').exists()).toBe(true);
  });

  it('says 1 second and 1 call', async () => {
    serve(two, () => ({ servers: [status('ambeserver:2460', 1, 1), status('thumbdv.example.org:2460', 1, 1, 1)] }));
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    expect(statusCell(wrapper, 0).text()).toBe('Answered 1 second ago. 1 call now.');
  });

  it('says a server that is not saved yet is checked after the save', async () => {
    serve(two, () => ({ servers: [status('ambeserver:2460', 2, 3), status('thumbdv.example.org:2460', 1, 0, 1)] }));
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    await wrapper.find('[data-test="ambe-server-add"]').trigger('click');
    const cell = statusCell(wrapper, 2);
    expect(cell.text()).toBe('Checked after you save.');
    expect(cell.find('.text-muted-foreground').exists()).toBe(true);
    expect(cell.find('.rounded-full').exists()).toBe(false);
    await wrapper.find('[data-test="ambe-server-address-2"]').setValue('ambeserver:2460');
    expect(statusCell(wrapper, 2).text()).toBe('Answered 2 seconds ago. 3 calls now.');
  });

  it('shows no status when the status cannot be read', async () => {
    mockAPI.get.mockImplementation((url: string) => (url === '/config/transcoding'
      ? Promise.reject(new Error('no'))
      : Promise.resolve(config(two))));
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    expect(statusCell(wrapper, 0).text()).toBe('');
  });

  it('refreshes the status every 5 seconds until the page closes', async () => {
    vi.useFakeTimers();
    let seconds = 2;
    serve(two, () => ({ servers: [status('ambeserver:2460', seconds, 1)] }));
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    expect(statusCalls()).toBe(1);
    expect(statusCell(wrapper, 0).text()).toBe('Answered 2 seconds ago. 1 call now.');
    seconds = 7;
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(statusCalls()).toBe(2);
    expect(statusCell(wrapper, 0).text()).toBe('Answered 7 seconds ago. 1 call now.');
    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(15000);
    expect(statusCalls()).toBe(2);
  });

  it('refreshes the status right after a save', async () => {
    let answer: TranscodingStatus = { servers: [status('ambeserver:2460', 3, 0)] };
    const saved = [...two];
    serve(saved, () => answer);
    accept();
    const wrapper = mountWizard('/admin/setup');
    await flushPromises();
    expect(statusCalls()).toBe(1);
    await wrapper.find('[data-test="ambe-server-add"]').trigger('click');
    await wrapper.find('[data-test="ambe-server-address-2"]').setValue('new.example.org:2460');
    expect(statusCell(wrapper, 2).text()).toBe('Checked after you save.');
    answer = { servers: [...answer.servers, status('new.example.org:2460', 0, 0)] };
    saved.push({ address: 'new.example.org:2460', calls: 0 });
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(statusCalls()).toBe(2);
    expect(statusCell(wrapper, 2).text()).toBe('Answered 0 seconds ago. 0 calls now.');
  });
});
