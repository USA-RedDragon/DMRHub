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
  default: { get: vi.fn(), patch: vi.fn() },
}));

import API from '@/services/API';
import TalkgroupAccessCard from '@/components/TalkgroupAccessCard.vue';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

const ACCESS = {
  mode: 'approved',
  repeaters: [{ id: 311860, callsign: 'KI5VMF' }, { id: 311861, callsign: 'W5ABC' }],
};
const CALLSIGNS: Record<number, string> = { 311860: 'KI5VMF', 311861: 'W5ABC', 319186801: 'KI5VMF' };

async function card(access: Record<string, unknown> = ACCESS) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/talkgroups/31665/access') return Promise.resolve({ data: access });
    if (url === '/repeaters/319186801') return Promise.resolve({ data: { id: 319186801, callsign: 'KI5VMF' } });
    return Promise.reject({ response: { status: 400, data: { error: 'Repeater does not exist' } } });
  });
  mockAPI.patch.mockImplementation((_url: string, body: Record<string, unknown>) => Promise.resolve({
    data: { mode: body.mode, repeaters: (body.repeaters as number[]).map((id) => ({ id, callsign: CALLSIGNS[id] })) },
  }));
  const wrapper = mount(TalkgroupAccessCard, { props: { talkgroupId: 31665 } });
  await flushPromises();
  return wrapper;
}

function radio(wrapper: Awaited<ReturnType<typeof card>>, value: string) {
  return wrapper.find(`input[name="tg-access"][value="${value}"]`);
}

function chips(wrapper: Awaited<ReturnType<typeof card>>) {
  return wrapper.find('[data-test="access-repeaters"]').findAll('.chip').map((c) => c.text());
}

describe('TalkgroupAccessCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the approved repeaters', async () => {
    const wrapper = await card();
    expect(wrapper.text()).toContain('Who can link');
    expect((radio(wrapper, 'approved').element as HTMLInputElement).checked).toBe(true);
    expect(chips(wrapper)).toEqual(['311860KI5VMF×', '311861W5ABC×']);
  });

  it('shows no repeater list for an open talkgroup', async () => {
    const wrapper = await card({ mode: 'open', repeaters: [] });
    expect((radio(wrapper, 'open').element as HTMLInputElement).checked).toBe(true);
    expect(wrapper.find('[data-test="access-repeaters"]').exists()).toBe(false);
    expect(wrapper.find('[data-test="add-repeater"]').exists()).toBe(false);
  });

  it('adds a repeater by ID, removes one and saves the IDs', async () => {
    const wrapper = await card();
    await wrapper.find('[data-test="add-repeater"]').setValue('319186801');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(chips(wrapper)).toContain('319186801KI5VMF×');
    await wrapper.find('button[aria-label="Remove 311861"]').trigger('click');
    await wrapper.find('[data-test="access-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/talkgroups/31665/access', { mode: 'approved', repeaters: [311860, 319186801] });
    expect(wrapper.find('[data-test="access-message"]').text()).toBe('Saved.');
    expect(chips(wrapper)).toEqual(['311860KI5VMF×', '319186801KI5VMF×']);
  });

  it('shows an unknown repeater and adds nothing', async () => {
    const wrapper = await card();
    for (const q of ['999999', 'W5ABC']) {
      await wrapper.find('[data-test="add-repeater"]').setValue(q);
      await wrapper.find('form').trigger('submit');
      await flushPromises();
      expect(wrapper.text()).toContain('No such repeater');
      expect(wrapper.find('[data-test="add-repeater"]').attributes('aria-invalid')).toBe('true');
    }
    expect(chips(wrapper)).toHaveLength(2);
  });

  it('opens the talkgroup', async () => {
    const wrapper = await card();
    await radio(wrapper, 'open').trigger('change');
    expect(wrapper.find('[data-test="access-repeaters"]').exists()).toBe(false);
    await wrapper.find('[data-test="access-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch.mock.calls[0][1]).toMatchObject({ mode: 'open' });
  });

  it('resets the draft to the saved access', async () => {
    const wrapper = await card();
    await wrapper.find('button[aria-label="Remove 311860"]').trigger('click');
    await radio(wrapper, 'open').trigger('change');
    await wrapper.find('[data-test="access-reset"]').trigger('click');
    expect((radio(wrapper, 'approved').element as HTMLInputElement).checked).toBe(true);
    expect(chips(wrapper)).toHaveLength(2);
  });

  it('shows the error of a failed save', async () => {
    const wrapper = await card();
    mockAPI.patch.mockRejectedValueOnce({ response: { status: 400, data: { error: 'Repeater does not exist' } } });
    await wrapper.find('[data-test="access-save"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="access-message"]').text()).toBe('Repeater does not exist');
  });
});
