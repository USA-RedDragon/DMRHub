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
import { createPinia } from 'pinia';
import type { Message } from '@/services/messages';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

const socket = vi.hoisted(() => ({
  handler: null as null | ((event: { data: string }) => void),
  url: '',
  closed: false,
}));

vi.mock('@/services/ws', () => ({
  default: {
    connect: (url: string, handler: (event: { data: string }) => void) => {
      socket.url = url;
      socket.handler = handler;
      return { close: () => { socket.closed = true; } };
    },
  },
}));

import API from '@/services/API';
import MessagesPage from '@/views/messages/MessagesPage.vue';

const mockAPI = API as unknown as { get: Mock };

const privacy =
  'When SMS Storage is on, the text messages sent to and from the radio IDs that you own appear on your ' +
  'Messages page, up to the 250 newest in each box.';

function msg(id: number, over: Partial<Message> = {}): Message {
  return {
    id,
    received_at: new Date(Date.now() - id * 60000).toISOString(),
    source_id: 3140598,
    source_callsign: 'KD5SMP',
    destination_id: 3191868,
    destination_callsign: 'KI5VMF',
    group: false,
    talkgroup_name: '',
    format: 'ip',
    text: `message ${id}`,
    confirmed: true,
    response: 'ack',
    checksum_ok: true,
    octets: 9,
    ...over,
  };
}

type Fixture = {
  smsStorage: boolean;
  inbox?: { total: number; messages: Message[] };
  outbox?: { total: number; messages: Message[] };
  stats?: Record<string, unknown>;
};

function serve(f: Fixture) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/users/me/settings') {
      return Promise.resolve({ data: { sms_storage: f.smsStorage } });
    }
    if (url === '/users/me/radio-ids') {
      return Promise.resolve({
        data: {
          radio_ids: [
            { id: 3191868, callsign: 'KI5VMF', account: true, verified_at: null },
            { id: 3191877, callsign: 'KI5VMF', account: false, verified_at: '2026-10-10T20:41:37Z' },
          ],
        },
      });
    }
    if (url.startsWith('/messages/inbox')) {
      return Promise.resolve({ data: f.inbox ?? { total: 0, messages: [] } });
    }
    if (url.startsWith('/messages/outbox')) {
      return Promise.resolve({ data: f.outbox ?? { total: 0, messages: [] } });
    }
    if (url.startsWith('/messages/stats')) {
      return Promise.resolve({
        data: f.stats ?? { inbox: 0, outbox: 0, ack: 0, nack: 0, by_format: {}, by_day: [] },
      });
    }
    return Promise.reject({ response: { status: 404 } });
  });
}

async function mountPage() {
  const wrapper = mount(MessagesPage, {
    attachTo: document.body,
    global: {
      plugins: [createPinia()],
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return wrapper;
}

describe('MessagesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    socket.handler = null;
    socket.url = '';
    socket.closed = false;
    document.body.innerHTML = '';
  });

  it('asks a visitor who is not logged in to log in and loads no messages', async () => {
    mockAPI.get.mockRejectedValue({ response: { status: 401, data: { error: 'Authentication failed' } } });
    const wrapper = await mountPage();
    const card = wrapper.find('[data-test="messages-anon"]');
    expect(card.text()).toContain('Log in to see your messages');
    expect(card.text()).toContain('Messages are private to the owner of each radio ID.');
    expect(card.find('a').attributes('href')).toBe('/login');
    expect(mockAPI.get.mock.calls.some(([url]) => String(url).startsWith('/messages'))).toBe(false);
    expect(socket.handler).toBeNull();
  });

  it('shows the off state with the privacy text and loads no messages', async () => {
    serve({ smsStorage: false });
    const wrapper = await mountPage();
    const card = wrapper.find('[data-test="messages-off"]');
    expect(card.text()).toContain('SMS Storage is off');
    expect(card.find('[data-test="sms-privacy"]').text()).toBe(privacy);
    expect(card.find('a').attributes('href')).toBe('/settings');
    expect(wrapper.find('[data-test="tab-inbox"]').exists()).toBe(false);
    expect(mockAPI.get.mock.calls.some(([url]) => String(url).startsWith('/messages'))).toBe(false);
    expect(socket.handler).toBeNull();
  });

  it('shows the inbox, the counts and the stats when SMS Storage is on', async () => {
    serve({
      smsStorage: true,
      inbox: {
        total: 2,
        messages: [
          msg(1, { checksum_ok: false, format: 'hytera', response: 'none' }),
          msg(2, { text: null, format: 'data', octets: 92 }),
        ],
      },
      outbox: { total: 1, messages: [msg(3, { group: true, destination_id: 3100, destination_callsign: '' })] },
      stats: {
        inbox: 2,
        outbox: 1,
        ack: 2,
        nack: 0,
        by_format: { ip: 1, hytera: 1, data: 1 },
        by_day: [
          { day: '2026-10-21', count: 0 },
          { day: '2026-10-22', count: 3 },
        ],
      },
    });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="messages-ids"]').text()).toContain('(3191868, 3191877)');
    expect(wrapper.find('[data-test="tab-inbox"]').text()).toBe('Inbox2');
    expect(wrapper.find('[data-test="tab-outbox"]').text()).toBe('Outbox1');
    const text = wrapper.text();
    expect(text).toContain('Only the 250 most recent messages are shown.');
    expect(text).toContain('showing 2 · this box shows the 250 most recent messages');
    expect(text).toContain('Checksum bad');
    expect(text).toContain('Confirmed, no reply');
    expect(text).toContain('Not text: 92 octets of data');
    expect(wrapper.find('[data-test="stat-ack"]').text()).toContain('2');
    const byFormat = wrapper.find('[data-test="stats-by-format"]').text();
    expect(byFormat).toContain('ETSI IP');
    expect(byFormat).toContain('Hytera');
    expect(byFormat).not.toContain('Motorola TMS');
    expect(wrapper.find('[data-test="stats-by-day"]').text()).toContain('Oct 22');
    expect(socket.url).toMatch(/\/ws\/messages$/);
  });

  it('switches to the outbox, which holds group messages', async () => {
    serve({
      smsStorage: true,
      inbox: { total: 1, messages: [msg(1)] },
      outbox: { total: 1, messages: [msg(3, { group: true, destination_id: 3100, destination_callsign: '' })] },
    });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="table-outbox"]').exists()).toBe(false);
    await wrapper.find('[data-test="tab-outbox"]').trigger('mousedown', { button: 0 });
    await flushPromises();
    expect(wrapper.find('[data-test="table-inbox"]').exists()).toBe(false);
    const outbox = wrapper.find('[data-test="table-outbox"]');
    expect(outbox.text()).toContain('Group');
    expect(wrapper.text()).toContain('Messages sent from your radio IDs, including group messages to talkgroups.');
  });

  it('caps the counts and the pages at 250', async () => {
    serve({
      smsStorage: true,
      inbox: { total: 400, messages: Array.from({ length: 25 }, (_, i) => msg(i + 1)) },
      stats: { inbox: 400, outbox: 300, ack: 0, nack: 0, by_format: {}, by_day: [] },
    });
    const wrapper = await mountPage();
    expect(wrapper.find('[data-test="tab-inbox"]').text()).toBe('Inbox250');
    expect(wrapper.find('[data-test="tab-outbox"]').text()).toBe('Outbox250');
    expect(wrapper.find('[data-test="stat-inbox"]').text()).toContain('250');
    expect(wrapper.find('[data-test="stat-outbox"]').text()).toContain('250');
    expect(wrapper.text()).toContain('Page 1 of 10');
    expect(wrapper.text()).not.toMatch(/\b(300|400)\b/);
  });

  it('shows the empty boxes and no dates or hints about older messages', async () => {
    serve({ smsStorage: true });
    const wrapper = await mountPage();
    expect(wrapper.text()).toContain('No messages yet');
    expect(wrapper.find('[data-test="stats-by-format"]').text()).toContain('None');
    expect(wrapper.text()).not.toMatch(/since|older|earlier|before|records all|dmr\./i);
  });

  it('does not count a change to a message on another page as new', async () => {
    serve({ smsStorage: true, inbox: { total: 2, messages: [msg(10), msg(9)] } });
    const wrapper = await mountPage();
    socket.handler!({ data: JSON.stringify({ boxes: ['inbox'], message: msg(3, { response: 'nack' }) }) });
    await flushPromises();
    expect(wrapper.text()).toContain('showing 2 ·');
    expect(wrapper.text()).not.toContain('message 3');
  });

  it('does not connect the websocket after the page is gone', async () => {
    serve({ smsStorage: true });
    const wrapper = mount(MessagesPage, {
      global: {
        plugins: [createPinia()],
        stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
      },
    });
    wrapper.unmount();
    await flushPromises();
    expect(socket.handler).toBeNull();
  });

  it('asks for the stats in UTC when the server does not know the browser time zone', async () => {
    serve({ smsStorage: true, stats: { inbox: 3, outbox: 0, ack: 0, nack: 0, by_format: {}, by_day: [] } });
    const base = mockAPI.get.getMockImplementation()!;
    mockAPI.get.mockImplementation((url: string) => {
      if (url.startsWith('/messages/stats') && !url.endsWith('tz=UTC')) {
        return Promise.reject({ response: { status: 400, data: { error: 'Unknown time zone' } } });
      }
      return base(url);
    });
    const wrapper = await mountPage();
    expect(mockAPI.get).toHaveBeenCalledWith('/messages/stats?tz=UTC');
    expect(wrapper.find('[data-test="tab-inbox"]').text()).toBe('Inbox3');
  });

  it('adds a pushed message to the inbox', async () => {
    serve({ smsStorage: true, inbox: { total: 1, messages: [msg(1)] } });
    const wrapper = await mountPage();
    socket.handler!({ data: JSON.stringify({ boxes: ['inbox'], message: msg(7, { text: 'pushed' }) }) });
    await flushPromises();
    expect(wrapper.text()).toContain('pushed');
    expect(wrapper.text()).toContain('showing 2');
    wrapper.unmount();
    expect(socket.closed).toBe(true);
  });
});
