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
import { createPinia, setActivePinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), patch: vi.fn(), post: vi.fn() },
}));
vi.mock('@/services/ws', () => ({
  default: { connect: vi.fn(() => ({ close: vi.fn() })) },
}));

import API from '@/services/API';
import RepeaterDetailsPage from '@/views/repeaters/RepeaterDetailsPage.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; post: Mock };

async function badge(lastPing: string, extra: Record<string, unknown> = {}) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/lastheard')) {
      return Promise.resolve({ data: { calls: [], total: 0 } });
    }
    return Promise.resolve({
      data: {
        id: 310000101, callsign: 'N0CALL', type: 'mmdvm', last_ping_time: lastPing,
        connected_time: lastPing, created_at: lastPing, ts1_static_talkgroups: [], ts2_static_talkgroups: [],
        ts1_dynamic_talkgroup: { id: 0 }, ts2_dynamic_talkgroup: { id: 0 },
        latitude: 0, longitude: 0, rx_frequency: 0, tx_frequency: 0, tx_power: 0, color_code: 1,
        ...extra,
      },
    });
  });
  const wrapper = mount(RepeaterDetailsPage, {
    global: {
      plugins: [createPinia()],
      mocks: { $route: { params: { id: '310000101' } }, $toast: { add: vi.fn() } },
      stubs: { RouterLink: true },
    },
  });
  await flushPromises();
  const text = wrapper.text();
  wrapper.unmount();
  return text;
}

describe('RepeaterDetailsPage online badge', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows Online after a ping within the session timeout', async () => {
    const text = await badge(new Date(Date.now() - 10_000).toISOString());
    expect(text).toContain('Online');
    expect(text).not.toContain('Offline');
  });

  it('shows Offline when the last ping is older than the session timeout', async () => {
    const text = await badge(new Date(Date.now() - 10 * 60_000).toISOString());
    expect(text).toContain('Offline');
  });
});

describe('RepeaterDetailsPage details', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the owner and when the repeater was made', async () => {
    const text = await badge(new Date(Date.now() - 10_000).toISOString(), {
      owner: { id: 3000001, callsign: 'N0DEMO' },
      created_at: new Date(Date.now() - 3 * 24 * 60 * 60_000).toISOString(),
    });
    expect(text).toMatch(/Owner\s*N0DEMO/);
    expect(text).toMatch(/Created\s*3 days ago/);
  });

  it('shows no rules to a visitor who does not own the repeater', async () => {
    const text = await badge(new Date(Date.now() - 10_000).toISOString(), { owner: { id: 3000001, callsign: 'N0DEMO' } });
    expect(text).not.toContain('Slot lock');
  });
});

async function asRole(role: string | null, userID: number, extra: Record<string, unknown> = {}) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/lastheard')) {
      return Promise.resolve({ data: { calls: [], total: 0 } });
    }
    if (url.endsWith('/cosysops')) {
      if (role === null) return Promise.reject({ response: { status: 403 } });
      return Promise.resolve({ data: { owner: { id: 3000001, callsign: 'N0DEMO' }, role, cosysops: [] } });
    }
    const t = new Date().toISOString();
    return Promise.resolve({
      data: {
        id: 310000101, callsign: 'N0CALL', type: 'mmdvm', last_ping_time: t, connected_time: t, created_at: t,
        ts1_static_talkgroups: [], ts2_static_talkgroups: [], ts1_dynamic_talkgroup: { id: 0 }, ts2_dynamic_talkgroup: { id: 0 },
        latitude: 0, longitude: 0, rx_frequency: 0, tx_frequency: 0, tx_power: 0, color_code: 1,
        owner: { id: 3000001, callsign: 'N0DEMO' },
        ...extra,
      },
    });
  });
  const pinia = createPinia();
  setActivePinia(pinia);
  const store = useUserStore();
  store.loggedIn = true;
  store.id = userID;
  const wrapper = mount(RepeaterDetailsPage, {
    global: {
      plugins: [pinia],
      mocks: { $route: { params: { id: '310000101' } }, $toast: { add: vi.fn() } },
      stubs: { RouterLink: true, RepeaterRulesCard: true },
    },
  });
  await flushPromises();
  return wrapper;
}

describe('RepeaterDetailsPage roles', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lets a co-sysop edit the codec but not connect or see the password', async () => {
    const w = await asRole('cosysop', 3000002);
    expect(w.find('[data-test="codec-save"]').exists()).toBe(true);
    expect(w.find('[data-test="connect"]').exists()).toBe(false);
    expect(w.find('[data-test="cosysops"]').text()).toContain('Owner: N0DEMO.');
    w.unmount();
  });

  it('lets the owner connect and manage co-sysops', async () => {
    const w = await asRole('owner', 3000001);
    expect(w.find('[data-test="codec-save"]').exists()).toBe(true);
    expect(w.find('[data-test="connect"]').exists()).toBe(true);
    expect(w.find('[data-test="invite-user"]').exists()).toBe(true);
    w.unmount();
  });

  it('shows another user no controls', async () => {
    const w = await asRole(null, 3000003);
    expect(w.find('[data-test="codec-save"]').exists()).toBe(false);
    expect(w.find('[data-test="connect"]').exists()).toBe(false);
    expect(w.find('[data-test="cosysops"]').exists()).toBe(false);
    w.unmount();
  });
});

describe('RepeaterDetailsPage sysop actions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('resets the connection of an online hotspot after a confirm', async () => {
    mockAPI.post.mockResolvedValue({ data: {} });
    const w = await asRole('cosysop', 3000002);
    await w.find('[data-test="reset-connection"]').trigger('click');
    await flushPromises();
    const dialog = document.querySelector('[data-test="reset-dialog"]');
    expect(dialog?.textContent).toContain('The hotspot logs in again by itself.');
    (document.querySelector('[data-test="reset-confirm"]') as HTMLElement).click();
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/310000101/reset');
    w.unmount();
  });

  it('drops the dynamic talkgroup of a timeslot', async () => {
    mockAPI.post.mockResolvedValue({ data: {} });
    const w = await asRole('owner', 3000001, { ts2_dynamic_talkgroup: { id: 3148, name: 'Texas' } });
    expect(w.find('[data-test="drop-dynamic-ts1"]').exists()).toBe(false);
    await w.find('[data-test="drop-dynamic-ts2"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/310000101/unlink/dynamic/2/3148', {});
    w.unmount();
  });

  it('shows no sysop actions to another user', async () => {
    const w = await asRole(null, 3000003);
    expect(w.find('[data-test="reset-connection"]').exists()).toBe(false);
    expect(mockAPI.get).not.toHaveBeenCalledWith('/repeaters/310000101/on-air');
    w.unmount();
  });
});
