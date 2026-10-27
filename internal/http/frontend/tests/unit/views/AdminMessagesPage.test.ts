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

import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia } from 'pinia';
import type { AdminMessage } from '@/services/messages';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import AdminMessagesPage from '@/views/admin/MessagesPage.vue';

const mockAPI = API as unknown as { get: Mock };

function msg(id: number, over: Partial<AdminMessage> = {}): AdminMessage {
  return {
    id,
    received_at: '2026-10-22T22:21:05Z',
    source_id: 3140598,
    source_callsign: 'KD5SMP',
    destination_id: 3191868,
    destination_callsign: 'KI5VMF',
    group: false,
    talkgroup_name: '',
    format: 'ip',
    text: 'TEST',
    confirmed: true,
    response: 'ack',
    checksum_ok: true,
    transmission_id: 4711,
    repeater_id: 314059801,
    slot: 2,
    transport: 'mmdvm',
    stream_id: 1,
    sap: 4,
    bad_crc: false,
    blocks: 3,
    octets: 44,
    ...over,
  };
}

function serve(list: { total: number; messages: AdminMessage[] }) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/admin/messages?')) {
      return Promise.resolve({ data: list });
    }
    const m = url.match(/^\/admin\/messages\/(\d+)$/);
    if (m) {
      const found = list.messages.find((x) => x.id === Number(m[1]));
      if (!found) {
        return Promise.reject({ response: { status: 404, data: { error: 'Message not found' } } });
      }
      return Promise.resolve({ data: { ...found, raw: '4045' + '00'.repeat(16) + '54455354' } });
    }
    return Promise.reject({ response: { status: 404 } });
  });
}

async function mountPage() {
  const wrapper = mount(AdminMessagesPage, {
    attachTo: document.body,
    global: {
      plugins: [createPinia()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return wrapper;
}

const searchCalls = () => mockAPI.get.mock.calls.map(([u]) => String(u)).filter((u) => u.startsWith('/admin/messages?'));

describe('Admin MessagesPage', () => {
  const tz = process.env.TZ;
  beforeAll(() => {
    process.env.TZ = 'America/Chicago';
  });
  afterAll(() => {
    process.env.TZ = tz;
  });

  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('shows admins only to a user who is not an admin', async () => {
    mockAPI.get.mockRejectedValue({ response: { status: 401, data: { error: 'Authentication failed' } } });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="admin-messages-denied"]').text()).toContain('Admins only');
    expect(wrapper.find('[data-test="admin-search"]').exists()).toBe(false);
  });

  it('shows example IDs that belong to no one', async () => {
    serve({ total: 0, messages: [] });
    const wrapper = await mountPage();
    expect(wrapper.find('#f-src').attributes('placeholder')).toBe('3000001');
    expect(wrapper.find('#f-dst').attributes('placeholder')).toBe('3000002 or 3100');
  });

  it('lists every stored message with no 250 cap', async () => {
    serve({ total: 1234, messages: [msg(1), msg(2)] });
    const wrapper = await mountPage();
    expect(searchCalls()[0]).toBe('/admin/messages?page=1&limit=25');
    const text = wrapper.text();
    expect(text).toContain('Page 1 of 50');
    expect(text).toContain('2 of 1234 stored messages');
    expect(text).toContain('MMDVM');
    expect(text).toContain('Select a row to see the raw message.');
  });

  it('shows the empty state when nothing matches', async () => {
    serve({ total: 0, messages: [] });
    const wrapper = await mountPage();
    expect(wrapper.text()).toContain('No messages match these filters');
    expect(wrapper.text()).toContain('0 of 0 stored messages');
  });

  it('sends the filters, with local days as times', async () => {
    serve({ total: 1, messages: [msg(1)] });
    const wrapper = await mountPage();
    await wrapper.find('#f-src').setValue(' 3191868 ');
    await wrapper.find('#f-dst').setValue('3100');
    await wrapper.find('#f-from').setValue('2026-10-01');
    await wrapper.find('#f-to').setValue('2026-10-22');
    await wrapper.find('#f-text').setValue('net');
    await wrapper.find('[data-test="admin-search"]').trigger('submit');
    await flushPromises();
    const q = new URLSearchParams(searchCalls().at(-1)!.split('?')[1]);
    expect(q.get('src')).toBe('3191868');
    expect(q.get('dst')).toBe('3100');
    expect(q.get('text')).toBe('net');
    expect(q.get('from')).toBe('2026-10-01T05:00:00.000Z');
    expect(q.get('to')).toBe('2026-10-23T05:00:00.000Z');
    expect(q.has('format')).toBe(false);
    expect(wrapper.text()).toContain('1 of 1 matching messages');
  });

  it('shows the server error for a bad search', async () => {
    serve({ total: 0, messages: [] });
    const wrapper = await mountPage();
    mockAPI.get.mockRejectedValueOnce({ response: { status: 400, data: { error: 'src must be a number' } } });
    await wrapper.find('#f-src').setValue('abc');
    await wrapper.find('[data-test="admin-search"]').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="admin-search-error"]').text()).toBe('src must be a number');
  });

  it('shows the raw message of the row that is clicked', async () => {
    serve({ total: 2, messages: [msg(1), msg(2, { transport: 'ipsc', slot: 1, sap: 9, checksum_ok: false })] });
    const wrapper = await mountPage();
    const rows = wrapper.findAll('[data-test="admin-table"] tbody tr');
    await rows[1]!.trigger('click');
    await flushPromises();
    const detail = wrapper.find('[data-test="admin-message-detail"]');
    expect(detail.text()).toContain('Message 2');
    expect(detail.text()).toContain('Proprietary (9)');
    expect(detail.text()).toContain('Bad');
    expect(detail.text()).toContain('IPSC / 314059801 / TS1');
    expect(wrapper.find('[data-test="admin-raw"]').text()).toBe(
      '0000  40 45 00 00 00 00 00 00 00 00 00 00 00 00 00 00\n0010  00 00 54 45 53 54',
    );
    expect(rows[1]!.attributes('data-state')).toBe('selected');
  });
});
