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
import { tabPaths, TABS } from '@/lib/setupMenu';

const mockAPI = API as unknown as { get: Mock; post: Mock; put: Mock };

const plain = { editable: true, locked: false, start_only: false, source: 'default', not_in_effect: false, disagree: false };

const modem = (over: Record<string, unknown> = {}) => ({
  'enabled': true, 'port': '/dev/ttyAMA0', 'baud': 115200, 'repeater-id': 300000102, 'duplex': false, 'color-code': 1,
  'rx-frequency': 438800000, 'tx-frequency': 438800000, 'cw-id': { enabled: false }, 'beacons': { enabled: false }, ...over,
});

const configOf = (m: Record<string, unknown>, settings: Record<string, unknown> = {}) => ({
  data: {
    'network-name': 'Example DMR Network',
    'dmr': { modem: m, transcoding: { 'ambe-servers': [] } },
    'secrets': {},
    'revision': 41,
    'disagree': false,
    'settings': {
      ...Object.fromEntries(tabPaths(TABS.find((t) => t.id === 'modem')!).map((p) => [p, plain])),
      ...settings,
    },
    'replicas': [{ id: 'a41c00000000000a', self: true, revision: 41, current: true, at: '2026-10-27T07:41:07Z', apply_errors: [], invalid: [] }],
  },
});

type Answers = { config?: unknown; ports?: unknown; state?: unknown };

const answer = (a: Answers) => {
  mockAPI.get.mockImplementation((url: string) => {
    switch (url) {
      case '/config/modem/ports':
        return Promise.resolve({ data: a.ports ?? { replicas: [{ replica: 'a41c00000000000a', self: true, ports: [] }] } });
      case '/config/modem':
        return Promise.resolve({ data: a.state ?? { status: null, self: false } });
      default:
        return Promise.resolve(a.config ?? configOf(modem()));
    }
  });
};

const mountModem = async (): Promise<VueWrapper> => {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/setup', component: SetupPage }] });
  await router.push('/admin/setup#modem');
  await router.isReady();
  const wrapper = mount(SetupPage, { global: { plugins: [router], mocks: { $toast: { add: vi.fn() } } }, attachTo: document.body });
  await flushPromises();
  return wrapper;
};

const ports = { replicas: [{ replica: 'a41c00000000000a', self: true, ports: [{ path: '/dev/serial/by-id/usb-MMDVM_HS-if00', device: '/dev/ttyACM0' }, { path: '/dev/ttyAMA0' }] }] };

const twoReplicas = {
  replicas: [
    { replica: 'a41c00000000000a', self: true, ports: [{ path: '/dev/ttyAMA0' }] },
    { replica: '7f3c00000000000b', self: false, ports: [{ path: '/dev/ttyAMA0' }, { path: '/dev/ttyACM0' }] },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  window.scrollTo = vi.fn() as unknown as typeof window.scrollTo;
  answer({});
});

describe('Modem tab serial port', () => {
  it('lists the ports that the server finds', async () => {
    answer({ ports });
    const wrapper = await mountModem();
    const select = wrapper.find('[data-test="modem-port-select"]');
    expect(select.exists()).toBe(true);
    expect(select.findAll('option').map((o) => o.text())).toEqual([
      '/dev/serial/by-id/usb-MMDVM_HS-if00 (/dev/ttyACM0)', '/dev/ttyAMA0', 'Another path',
    ]);
    expect((select.element as HTMLSelectElement).value).toBe('/dev/ttyAMA0');
    expect(wrapper.find('[data-test="modem-port-path"]').exists()).toBe(false);
    expect(wrapper.text()).toContain('The list shows the ports that this server finds.');
    await select.setValue('/dev/serial/by-id/usb-MMDVM_HS-if00');
    expect(wrapper.find('[data-test="menu-dirty-modem"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('takes a path that is not listed', async () => {
    answer({ ports });
    const wrapper = await mountModem();
    await wrapper.find('[data-test="modem-port-select"]').setValue('\u0000other');
    const path = wrapper.find('[data-test="modem-port-path"]');
    expect(path.exists()).toBe(true);
    expect((path.element as HTMLInputElement).value).toBe('/dev/ttyAMA0');
    await path.setValue('/dev/ttyS3');
    expect((wrapper.find('[data-test="modem-port-select"]').element as HTMLSelectElement).value).toBe('\u0000other');
    expect((wrapper.find('[data-test="modem-port-path"]').element as HTMLInputElement).value).toBe('/dev/ttyS3');
    expect(wrapper.find('[data-test="menu-dirty-modem"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it('shows a box for the path when the server finds no port', async () => {
    const wrapper = await mountModem();
    expect(wrapper.find('[data-test="modem-port-select"]').exists()).toBe(false);
    expect((wrapper.find('#setup-dmr-modem-port').element as HTMLInputElement).value).toBe('/dev/ttyAMA0');
    expect(wrapper.text()).not.toContain('The list shows');
    wrapper.unmount();
  });

  it('locks the port that the deployment sets', async () => {
    answer({ ports, config: configOf(modem(), { 'dmr.modem.port': { ...plain, editable: false, locked: true, source: 'env', origin: 'DMR_MODEM_PORT' } }) });
    const wrapper = await mountModem();
    expect(wrapper.find('[data-test="modem-port-select"]').attributes('disabled')).toBeDefined();
    wrapper.unmount();
  });
});

describe('Modem tab with two replicas', () => {
  it('groups the ports by replica and tests on the replica of the chosen port', async () => {
    answer({ ports: twoReplicas });
    mockAPI.post.mockResolvedValue({ data: { replica: '7f3c00000000000b', self: false, live: false, result: { port: '/dev/ttyACM0', baud: 115200, opened: true, tries: 1, answered: true, frequencies: { taken: true }, settings: { taken: true } } } });
    const wrapper = await mountModem();
    const select = wrapper.find('[data-test="modem-port-select"]');
    expect(select.findAll('optgroup').map((g) => g.attributes('label'))).toEqual(['Replica a41c', 'Replica 7f3c']);
    expect((select.element as HTMLSelectElement).value).toBe('a41c00000000000a\u0001/dev/ttyAMA0');
    expect(wrapper.text()).toContain('The list shows the ports that each replica finds.');
    await select.setValue('7f3c00000000000b\u0001/dev/ttyACM0');
    expect((wrapper.find('[data-test="modem-port-select"]').element as HTMLSelectElement).value).toBe('7f3c00000000000b\u0001/dev/ttyACM0');
    await wrapper.find('[data-test="modem-test-run"]').trigger('click');
    await flushPromises();
    const [, body, opts] = mockAPI.post.mock.calls[0]!;
    expect(body).toMatchObject({ port: '/dev/ttyACM0' });
    expect(opts).toEqual({ params: { replica: '7f3c00000000000b' } });
    expect(wrapper.find('[data-test="modem-test-result"]').text()).toContain('Replica7f3c');
    wrapper.unmount();
  });

  it('asks for the replica of a typed path', async () => {
    answer({ ports: twoReplicas });
    const wrapper = await mountModem();
    await wrapper.find('[data-test="modem-port-select"]').setValue('\u0000other');
    const replica = wrapper.find('[data-test="modem-port-replica"]');
    expect(replica.findAll('option').map((o) => o.text())).toEqual(['Replica a41c', 'Replica 7f3c']);
    await replica.setValue('7f3c00000000000b');
    mockAPI.post.mockResolvedValue({ data: { replica: '7f3c00000000000b', self: false, unanswered: true, live: false } });
    await wrapper.find('[data-test="modem-test-run"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post.mock.calls[0]![2]).toEqual({ params: { replica: '7f3c00000000000b' } });
    expect(wrapper.find('[data-test="modem-test-result"]').text()).toContain('Replica 7f3c did not answer');
    wrapper.unmount();
  });

  it('shows no replica choice with one replica', async () => {
    answer({ ports });
    const wrapper = await mountModem();
    expect(wrapper.find('optgroup').exists()).toBe(false);
    await wrapper.find('[data-test="modem-port-select"]').setValue('\u0000other');
    expect(wrapper.find('[data-test="modem-port-replica"]').exists()).toBe(false);
    wrapper.unmount();
  });
});

describe('Modem tab status pill', () => {
  it('shows the live state of the modem', async () => {
    answer({ state: { status: { replica: 'a41c00000000000a', port: '/dev/ttyAMA0', baud: 115200, linked: true }, self: true } });
    const wrapper = await mountModem();
    expect(wrapper.find('[data-test="modem-pill"]').text()).toBe('Modem connected');
    wrapper.unmount();
  });

  it('shows no pill and no test while the modem is off', async () => {
    answer({ config: configOf(modem({ enabled: false })) });
    const wrapper = await mountModem();
    expect(wrapper.find('[data-test="modem-pill"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="modem-test"]').exists()).toBe(false);
    wrapper.unmount();
  });
});

describe('Modem tab test', () => {
  it('tests the settings on the form without saving', async () => {
    mockAPI.post.mockResolvedValue({
      data: {
        live: false, result: {
          port: '/dev/ttyAMA0', baud: 57600, opened: true, tries: 1, answered: true, firmware: 'MMDVM_HS_Hat-v1.6.1', protocol: 1,
          modes: ['DMR', 'D-Star'], frequencies: { taken: true }, settings: { taken: true },
        },
      },
    });
    const wrapper = await mountModem();
    await wrapper.find('#setup-dmr-modem-baud').setValue('57600');
    await wrapper.find('[data-test="modem-test-run"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledTimes(1);
    const [url, body] = mockAPI.post.mock.calls[0]!;
    expect(url).toBe('/config/modem/test');
    expect(body).toMatchObject({ port: '/dev/ttyAMA0', baud: 57600, 'rx-frequency': 438800000 });
    expect(mockAPI.put).not.toHaveBeenCalled();
    const result = wrapper.find('[data-test="modem-test-result"]');
    expect(result.text()).toContain('The board answered');
    expect(result.text()).toContain('MMDVM_HS_Hat-v1.6.1');
    expect(result.text()).toContain('DMR, D-Star');
    expect(result.findAll('dd').map((d) => d.text()).slice(-2)).toEqual(['Taken', 'Taken']);
    wrapper.unmount();
  });

  it('says what to check when the board does not answer', async () => {
    mockAPI.post.mockResolvedValue({
      data: { live: false, result: { port: '/dev/ttyAMA0', baud: 115200, opened: true, tries: 6, answered: false, error: 'no version reply from the modem' } },
    });
    const wrapper = await mountModem();
    await wrapper.find('[data-test="modem-test-run"]').trigger('click');
    await flushPromises();
    const text = wrapper.find('[data-test="modem-test-result"]').text();
    expect(text).toContain('No answer from the board');
    expect(text).toContain('DMRHub opened /dev/ttyAMA0 at 115200 but the board did not answer after 6 tries.');
    expect(text).toContain('MMDVMHost');
    wrapper.unmount();
  });

  it('shows the live state while the modem runs', async () => {
    mockAPI.post.mockResolvedValue({
      data: { live: true, state: { status: { port: '/dev/ttyAMA0', baud: 115200, linked: true, firmware: 'MMDVM 20260101', protocol: 2, modes: ['DMR'] }, self: true } },
    });
    const wrapper = await mountModem();
    await wrapper.find('[data-test="modem-test-run"]').trigger('click');
    await flushPromises();
    const text = wrapper.find('[data-test="modem-test-result"]').text();
    expect(text).toContain('The modem is connected');
    expect(text).toContain('DMRHub did not open the port again.');
    wrapper.unmount();
  });

  it('names the settings that keep the test from running', async () => {
    mockAPI.post.mockRejectedValue({ response: { status: 400, data: { errors: { dmr: { modem: { port: 'the serial device of the modem is required when the modem is enabled' } } } } } });
    const wrapper = await mountModem();
    await wrapper.find('[data-test="modem-test-run"]').trigger('click');
    await flushPromises();
    const text = wrapper.find('[data-test="modem-test-result"]').text();
    expect(text).toContain('Fix these settings first');
    expect(text).toContain('Serial port: the serial device of the modem is required');
    wrapper.unmount();
  });
});
