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

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), patch: vi.fn() },
}));
vi.mock('@/services/ws', () => ({
  default: { connect: vi.fn(() => ({ close: vi.fn() })) },
}));

import API from '@/services/API';
import RepeaterDetailsPage from '@/views/repeaters/RepeaterDetailsPage.vue';

const mockAPI = API as unknown as { get: Mock };

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
