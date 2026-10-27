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
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
import PeerTable from '@/components/PeerTable.vue';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

class FakeSocket {
  addEventListener() {}
  send() {}
  close() {}
}

const peer = (enhanced: boolean, codec2 = false) => ({
  id: 500001,
  ingress: true,
  egress: true,
  enhanced,
  codec2,
  created_at: '2026-10-22T22:21:05Z',
  last_ping_time: '0001-01-01T00:00:00Z',
});

async function mountTable(admin: boolean, enhanced: boolean, codec2 = false) {
  mockAPI.get.mockResolvedValue({ data: { total: 1, peers: [peer(enhanced, codec2)] } });
  const toast = { add: vi.fn() };
  const wrapper = mount(PeerTable, {
    props: { admin },
    global: {
      mocks: { $toast: toast, $confirm: { require: vi.fn() } },
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return { wrapper, toast };
}

describe('PeerTable', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lets an admin turn on enhanced OpenBridge for a peer', async () => {
    mockAPI.patch.mockResolvedValue({ data: peer(true) });
    const { wrapper, toast } = await mountTable(true, false);
    const box = wrapper.find('input#enhanced');
    expect(box.exists()).toBe(true);
    expect((box.element as HTMLInputElement).disabled).toBe(false);
    await box.setValue(true);
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/500001', { enhanced: true });
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ severity: 'success' }));
  });

  it('shows the setting to an owner who is not an admin, without letting them change it', async () => {
    const { wrapper } = await mountTable(false, true);
    const box = wrapper.find('input#enhanced');
    expect((box.element as HTMLInputElement).checked).toBe(true);
    expect((box.element as HTMLInputElement).disabled).toBe(true);
  });

  it('puts the Codec2 column right after Enhanced', async () => {
    const { wrapper } = await mountTable(true, false);
    const headers = wrapper.findAll('th').map((th) => th.text());
    expect(headers[headers.indexOf('Enhanced') + 1]).toBe('Codec2');
  });

  it('lets an admin say that a peer carries Codec2', async () => {
    mockAPI.patch.mockResolvedValue({ data: peer(false, true) });
    const { wrapper, toast } = await mountTable(true, false);
    const box = wrapper.find('input#codec2');
    expect((box.element as HTMLInputElement).checked).toBe(false);
    expect((box.element as HTMLInputElement).disabled).toBe(false);
    expect(box.attributes('title')).toBe('This peer carries Codec2');
    expect(box.attributes('aria-label')).toBe('This peer carries Codec2');
    await box.setValue(true);
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/500001', { codec2: true });
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({
      severity: 'success',
      detail: 'The peer now gets calls in their own codec.',
    }));
  });

  it('lets an admin turn Codec2 off for a peer', async () => {
    mockAPI.patch.mockResolvedValue({ data: peer(false, false) });
    const { wrapper, toast } = await mountTable(true, false, true);
    await wrapper.find('input#codec2').setValue(false);
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/500001', { codec2: false });
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({
      severity: 'success',
      detail: 'The peer now gets every call as AMBE.',
    }));
  });

  it('puts the box back and shows an error when the save fails', async () => {
    mockAPI.patch.mockRejectedValue(new Error('no'));
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    const { wrapper, toast } = await mountTable(true, false);
    await wrapper.find('input#codec2').setValue(true);
    await flushPromises();
    expect((wrapper.find('input#codec2').element as HTMLInputElement).checked).toBe(false);
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({
      severity: 'error',
      detail: 'Error saving peer 500001',
    }));
    error.mockRestore();
  });

  it('shows the Codec2 setting to an owner who is not an admin, without letting them change it', async () => {
    const { wrapper } = await mountTable(false, false, true);
    const box = wrapper.find('input#codec2');
    expect((box.element as HTMLInputElement).checked).toBe(true);
    expect((box.element as HTMLInputElement).disabled).toBe(true);
  });

  it('shows the last ping of a peer', async () => {
    mockAPI.get.mockResolvedValue({
      data: { total: 1, peers: [{ ...peer(false), last_ping_time: new Date(Date.now() - 20_000).toISOString() }] },
    });
    const wrapper = mount(PeerTable, {
      props: { admin: true },
      global: {
        mocks: { $toast: { add: vi.fn() }, $confirm: { require: vi.fn() } },
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
      },
    });
    await flushPromises();
    expect(wrapper.find('tbody').text()).toContain('20 seconds ago');
    expect(wrapper.find('tbody').text()).not.toContain('Never');
  });

  it('links each peer to its page', async () => {
    const { wrapper } = await mountTable(true, false);
    const link = wrapper.find('a[href="/admin/peers/500001"]');
    expect(link.exists()).toBe(true);
    expect(link.text()).toBe('500001');
  });

  it('explains the Codec2 column under the table', async () => {
    const { wrapper } = await mountTable(false, false);
    expect(wrapper.find('[data-test="codec2-hint"]').text()).toBe(
      'Codec2: on, the peer gets every call in the codec it was sent in; turn it on only when every network behind the peer can carry Codec2. Off, the peer gets every call as AMBE.',
    );
  });
});
