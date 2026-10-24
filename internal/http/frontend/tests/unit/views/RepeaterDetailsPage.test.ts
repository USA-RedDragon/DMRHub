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
import { createPinia, setActivePinia, type Pinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    patch: vi.fn(),
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
import { Select } from '@/components/ui/select';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

let pinia: Pinia;

type Viewer = { loggedIn: boolean; id: number; admin: boolean };

const guest: Viewer = { loggedIn: false, id: 0, admin: false };
const owner: Viewer = { loggedIn: true, id: 1000001, admin: false };
const admin: Viewer = { loggedIn: true, id: 1000002, admin: true };
const other: Viewer = { loggedIn: true, id: 1000003, admin: false };

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

async function mountPage(fields: Record<string, unknown>, viewer: Viewer = guest) {
  Object.assign(useUserStore(), viewer);
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/repeaters/262523') {
      return Promise.resolve({ data: repeater(fields) });
    }
    return Promise.resolve({ data: { total: 0, calls: [] } });
  });
  const wrapper = mount(RepeaterDetailsPage, {
    global: {
      plugins: [pinia],
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
    pinia = createPinia();
    setActivePinia(pinia);
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

describe('RepeaterDetailsPage voice codec', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pinia = createPinia();
    setActivePinia(pinia);
  });

  const heard = {
    ts1_codec: 'auto',
    ts2_codec: 'auto',
    ts1_heard_codec: 'codec2',
    ts1_heard_codec2_mode: 3200,
    ts1_heard_at: '2026-10-23T19:02:00Z',
    ts2_heard_codec: 'ambe',
    ts2_heard_codec2_mode: 0,
    ts2_heard_at: '2026-10-23T18:47:30Z',
  };

  const selects = (wrapper: Awaited<ReturnType<typeof mountPage>>) => wrapper.findAllComponents(Select);

  it('shows the card with both timeslots and the hint', async () => {
    const wrapper = await mountPage(heard, owner);
    const card = wrapper.find('[data-test="voice-codec"]');
    expect(card.text()).toContain('Voice codec');
    expect(card.text()).toContain(
      "The codec this repeater's radios use on each timeslot. DMRHub sends AMBE calls to a Codec2 timeslot as Codec2, and Codec2 calls to AMBE targets as AMBE.",
    );
    expect(card.text()).toContain('Timeslot 1');
    expect(card.text()).toContain('Timeslot 2');
    expect(card.text()).toContain(
      'Automatic follows the last call heard on the timeslot. A timeslot counts as Codec2 only when a call starts with a Codec2 voice header; until then it is AMBE.',
    );
    expect(wrapper.find('label[for="ts1-codec"]').text()).toBe('Codec');
    expect(wrapper.find('#ts1-codec').exists()).toBe(true);
    expect(wrapper.find('#ts2-codec').exists()).toBe(true);
    expect(selects(wrapper).map((x) => x.props('modelValue'))).toEqual(['auto', 'auto']);
  });

  it('shows what was heard on each timeslot while it is automatic', async () => {
    const wrapper = await mountPage(heard, owner);
    expect(wrapper.find('[data-test="ts1-heard"]').text()).toMatch(/^Heard: Codec2 3200, on the last call at \d\d:\d\d \S+\.$/);
    expect(wrapper.find('[data-test="ts2-heard"]').text()).toMatch(/^Heard: AMBE, on the last call at \d\d:\d\d \S+\.$/);
  });

  it('shows nothing heard for a timeslot that was never heard', async () => {
    const wrapper = await mountPage({ ...heard, ts2_heard_codec: '', ts2_heard_at: null });
    expect(wrapper.find('[data-test="ts1-heard"]').exists()).toBe(true);
    expect(wrapper.find('[data-test="ts2-heard"]').exists()).toBe(false);
  });

  it('hides what was heard once a codec is chosen', async () => {
    const wrapper = await mountPage({ ...heard, ts2_codec: 'ambe' }, owner);
    expect(selects(wrapper).map((x) => x.props('modelValue'))).toEqual(['auto', 'ambe']);
    expect(wrapper.find('[data-test="ts2-heard"]').exists()).toBe(false);
    selects(wrapper)[0]!.vm.$emit('update:modelValue', 'codec2');
    await flushPromises();
    expect(wrapper.find('[data-test="ts1-heard"]').exists()).toBe(false);
  });

  for (const [who, viewer] of [['the owner', owner], ['an admin', admin]] as const) {
    it(`lets ${who} save the codecs`, async () => {
      mockAPI.patch.mockResolvedValue({ data: {} });
      const wrapper = await mountPage(heard, viewer);
      expect(selects(wrapper).every((x) => x.props('disabled') === false)).toBe(true);
      selects(wrapper)[0]!.vm.$emit('update:modelValue', 'codec2');
      await flushPromises();
      await wrapper.find('[data-test="codec-save"]').trigger('click');
      await flushPromises();
      expect(mockAPI.patch).toHaveBeenCalledWith('/repeaters/262523', { ts1_codec: 'codec2', ts2_codec: 'auto' });
      expect(wrapper.find('[data-test="codec-message"]').text()).toBe('Saved.');
      selects(wrapper)[1]!.vm.$emit('update:modelValue', 'ambe');
      await flushPromises();
      expect(wrapper.find('[data-test="codec-message"]').exists()).toBe(false);
    });
  }

  it('says when the save failed', async () => {
    mockAPI.patch.mockRejectedValue({ response: { status: 500, data: {} } });
    const wrapper = await mountPage(heard, owner);
    await wrapper.find('[data-test="codec-save"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="codec-message"]').text()).toBe('Could not save. Try again later.');
  });

  it('hides the card for an IPSC repeater', async () => {
    const wrapper = await mountPage({ ...heard, type: 'ipsc' }, admin);
    expect(wrapper.find('[data-test="voice-codec"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain('Voice codec');
  });

  it('shows the card for an MMDVM repeater', async () => {
    const wrapper = await mountPage({ ...heard, type: 'mmdvm' }, admin);
    expect(wrapper.find('[data-test="voice-codec"]').exists()).toBe(true);
  });

  for (const [who, viewer] of [['a guest', guest], ['another user', other]] as const) {
    it(`shows ${who} the codecs without letting them change them`, async () => {
      const wrapper = await mountPage(heard, viewer);
      expect(selects(wrapper).map((x) => x.props('disabled'))).toEqual([true, true]);
      expect(wrapper.find('[data-test="codec-save"]').exists()).toBe(false);
    });
  }
});
