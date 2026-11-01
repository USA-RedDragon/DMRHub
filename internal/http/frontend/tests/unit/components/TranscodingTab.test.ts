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

import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
  },
}));

import API from '@/services/API';
import TranscodingTab from '@/components/setup/menu/TranscodingTab.vue';
import type { ServerStatus, TranscodingStatus } from '@/lib/codec';

const mockAPI = API as unknown as { get: Mock };

const two = [
  { address: 'ambeserver:2460', calls: 0 },
  { address: 'ambe2.example.org:2460', calls: 1 },
];

const status = (server: string, answered: number | null, calls = 0, limit = 0): ServerStatus => ({
  server, calls_limit: limit, answered_seconds: answered, calls,
});

const serve = (answer: () => TranscodingStatus) => {
  mockAPI.get.mockImplementation(() => Promise.resolve({ data: answer() }));
};

const statusCalls = () => mockAPI.get.mock.calls.filter((c) => c[0] === '/config/transcoding').length;

const mountTab = async (props: Record<string, unknown> = {}): Promise<VueWrapper> => {
  const wrapper = mount(TranscodingTab, { props: { value: two, saves: 0, ...props } });
  await flushPromises();
  return wrapper;
};

const cell = (wrapper: VueWrapper, i: number) => wrapper.find(`[data-test="ambe-server-status-${i}"]`);

beforeEach(() => {
  vi.clearAllMocks();
  serve(() => ({ servers: [] }));
});

afterEach(() => {
  vi.useRealTimers();
});

describe('Transcoding tab of the Setup menu', () => {
  it('shows each server with its calls at once, and no limit as empty', async () => {
    const wrapper = await mountTab();
    expect((wrapper.find('[data-test="ambe-server-address-0"]').element as HTMLInputElement).value).toBe('ambeserver:2460');
    expect((wrapper.find('[data-test="ambe-server-calls-0"]').element as HTMLInputElement).value).toBe('');
    expect((wrapper.find('[data-test="ambe-server-calls-1"]').element as HTMLInputElement).value).toBe('1');
    expect(wrapper.text()).not.toMatch(/dmr\.|transcoding\.|ambe-servers/);
  });

  it('adds, edits and removes servers', async () => {
    const wrapper = await mountTab();
    await wrapper.find('[data-test="ambe-server-add"]').trigger('click');
    expect(wrapper.emitted('set')!.at(-1)).toEqual(['dmr.transcoding.ambe-servers', [...two, { address: '', calls: 1 }]]);
    await wrapper.find('[data-test="ambe-server-address-1"]').setValue('[::1]:2460');
    expect(wrapper.emitted('set')!.at(-1)).toEqual(['dmr.transcoding.ambe-servers', [two[0], { address: '[::1]:2460', calls: 1 }]]);
    await wrapper.find('[data-test="ambe-server-remove-0"]').trigger('click');
    expect(wrapper.emitted('set')!.at(-1)).toEqual(['dmr.transcoding.ambe-servers', [two[1]]]);
  });

  it('marks the bad fields of an error', async () => {
    const wrapper = await mountTab({ value: [{ address: 'x', calls: 1 }], error: 'Enter a host and port.' });
    expect(wrapper.find('[data-test="field-error"]').text()).toBe('Enter a host and port.');
    expect(wrapper.find('[data-test="ambe-server-address-0"]').attributes('aria-invalid')).toBe('true');
    expect(wrapper.find('[data-test="ambe-server-calls-0"]').attributes('aria-invalid')).toBeUndefined();
  });

  it('shows the status of each server by its address', async () => {
    serve(() => ({ servers: [status('ambe2.example.org:2460', null, 0, 1), status('ambeserver:2460', 2, 3)] }));
    const wrapper = await mountTab();
    expect(cell(wrapper, 0).text()).toContain('2');
    expect(cell(wrapper, 0).find('.bg-green-600').exists()).toBe(true);
    expect(cell(wrapper, 1).find('.bg-destructive-foreground').exists()).toBe(true);
  });

  it('refreshes the status every 5 seconds until it closes, and right after a save', async () => {
    vi.useFakeTimers();
    const wrapper = await mountTab();
    expect(statusCalls()).toBe(1);
    await vi.advanceTimersByTimeAsync(5000);
    expect(statusCalls()).toBe(2);
    await wrapper.setProps({ saves: 1 });
    await flushPromises();
    expect(statusCalls()).toBe(3);
    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(15000);
    expect(statusCalls()).toBe(3);
  });

  it('asks for no status and shows none in the setup wizard', async () => {
    vi.useFakeTimers();
    const wrapper = await mountTab({ wizard: true });
    await vi.advanceTimersByTimeAsync(15000);
    await wrapper.setProps({ saves: 1 });
    await flushPromises();
    expect(statusCalls()).toBe(0);
    expect(cell(wrapper, 0).exists()).toBe(false);
    expect(wrapper.find('[data-test="ambe-server-remove-0"]').exists()).toBe(true);
    wrapper.unmount();
  });
});
