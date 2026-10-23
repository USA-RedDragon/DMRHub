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
  },
}));

vi.mock('@/services/ws', () => ({
  default: {
    connect: vi.fn(() => ({ close: vi.fn() })),
  },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import RepeaterDetailsPage from '@/views/repeaters/RepeaterDetailsPage.vue';

const mockAPI = API as unknown as { get: Mock };

const repeater = (fields: Record<string, unknown>) => ({
  id: 262523,
  callsign: 'DL0HB',
  type: 'mmdvm',
  dialect: '',
  connected_time: '2026-10-23T18:00:00Z',
  last_ping_time: '2026-10-23T18:10:00Z',
  created_at: '2026-10-20T18:00:00Z',
  rx_frequency: 438000000,
  tx_frequency: 430400000,
  tx_power: 25,
  color_code: 1,
  latitude: 0,
  longitude: 0,
  height: 30,
  slots: 0,
  owner: { id: 1000001, callsign: 'DL1ABC' },
  ts1_static_talkgroups: [],
  ts2_static_talkgroups: [],
  ...fields,
});

async function mountPage(fields: Record<string, unknown>) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/repeaters/262523') {
      return Promise.resolve({ data: repeater(fields) });
    }
    return Promise.resolve({ data: { total: 0, calls: [] } });
  });
  const wrapper = mount(RepeaterDetailsPage, {
    global: {
      mocks: { $route: { params: { id: '262523' } } },
      stubs: { User: true, RelativeTimestamp: true },
    },
  });
  await flushPromises();
  return wrapper;
}

function row(wrapper: Awaited<ReturnType<typeof mountPage>>, label: string) {
  const dt = wrapper.findAll('dt').find((d) => d.text() === label);
  return dt?.element.nextElementSibling?.textContent?.trim();
}

describe('RepeaterDetailsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows an HBRP repeater as HBRP, without timeslots', async () => {
    const wrapper = await mountPage({ dialect: 'HBRP' });
    expect(row(wrapper, 'Protocol')).toBe('HBRP');
    expect(row(wrapper, 'Timeslots')).toBeUndefined();
    const badges = wrapper.findAll('span').map((s) => s.text());
    expect(badges).toContain('HBRP');
    expect(badges).not.toContain('MMDVM');
  });

  it('shows an MMDVM repeater as MMDVM, with its timeslots', async () => {
    const wrapper = await mountPage({ dialect: 'MMDVM', slots: 4 });
    expect(row(wrapper, 'Protocol')).toBe('MMDVM');
    expect(row(wrapper, 'Timeslots')).toBe('4');
    expect(wrapper.findAll('span').map((s) => s.text())).toContain('MMDVM');
  });

  it('shows no protocol for an IPSC repeater', async () => {
    const wrapper = await mountPage({ type: 'ipsc', slots: 3 });
    expect(row(wrapper, 'Protocol')).toBeUndefined();
    expect(wrapper.findAll('span').map((s) => s.text())).toContain('IPSC');
  });
});
