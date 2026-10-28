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
import RepeaterRulesCard from '@/components/RepeaterRulesCard.vue';

const mockAPI = API as unknown as { get: Mock; patch: Mock };

const RULES = {
  ts1_users: [],
  ts2_users: [{ id: 3191868, callsign: 'KI5VMF' }],
  talkgroup_mode: 'block',
  talkgroups: [{ id: 91, name: 'Worldwide' }],
};

async function card(rules: Record<string, unknown> = RULES) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/repeaters/311860/rules') return Promise.resolve({ data: rules });
    if (url === '/repeaters/311860/rules/user?q=n0call') return Promise.resolve({ data: { id: 3191869, callsign: 'N0CALL' } });
    if (url.startsWith('/repeaters/311860/rules/user')) return Promise.reject({ response: { status: 404, data: { error: 'User does not exist' } } });
    if (url === '/talkgroups/3100') return Promise.resolve({ data: { id: 3100, name: 'USA' } });
    return Promise.reject({ response: { status: 404, data: { error: 'Talkgroup does not exist' } } });
  });
  mockAPI.patch.mockImplementation((_url: string, body: Record<string, unknown>) => Promise.resolve({
    data: {
      ts1_users: (body.ts1_users as number[]).map((id) => ({ id, callsign: id === 3191869 ? 'N0CALL' : 'KI5VMF' })),
      ts2_users: (body.ts2_users as number[]).map((id) => ({ id, callsign: id === 3191869 ? 'N0CALL' : 'KI5VMF' })),
      talkgroup_mode: body.talkgroup_mode,
      talkgroups: (body.talkgroups as number[]).map((id) => ({ id, name: '' })),
    },
  }));
  const wrapper = mount(RepeaterRulesCard, { props: { repeaterId: 311860 } });
  await flushPromises();
  return wrapper;
}

function radio(wrapper: Awaited<ReturnType<typeof card>>, name: string, value: string) {
  return wrapper.find(`input[name="${name}"][value="${value}"]`);
}

describe('RepeaterRulesCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the saved rules', async () => {
    const wrapper = await card();
    expect((radio(wrapper, 'ts1-lock', 'open').element as HTMLInputElement).checked).toBe(true);
    expect((radio(wrapper, 'ts2-lock', 'lock').element as HTMLInputElement).checked).toBe(true);
    expect(wrapper.find('[data-test="ts2-users"]').text()).toContain('KI5VMF');
    expect(wrapper.find('[data-test="ts1-users"]').exists()).toBe(false);
    expect((radio(wrapper, 'tg-mode', 'block').element as HTMLInputElement).checked).toBe(true);
    expect(wrapper.find('[data-test="talkgroups"]').text()).toContain('Worldwide');
  });

  it('locks a slot to a user found by callsign and saves', async () => {
    const wrapper = await card();
    await radio(wrapper, 'ts1-lock', 'lock').trigger('change');
    await wrapper.find('[data-test="ts1-add-user"]').setValue('n0call');
    await wrapper.find('[data-test="ts1-lock"] form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="ts1-users"]').text()).toContain('N0CALL');
    await wrapper.find('[data-test="rules-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/repeaters/311860/rules', {
      ts1_users: [3191869], ts2_users: [3191868], talkgroup_mode: 'block', talkgroups: [91],
    });
    expect(wrapper.find('[data-test="rules-message"]').text()).toBe('Saved.');
  });

  it('shows an unknown user and adds nothing', async () => {
    const wrapper = await card();
    await wrapper.find('[data-test="ts2-add-user"]').setValue('N0NE');
    await wrapper.find('[data-test="ts2-lock"] form').trigger('submit');
    await flushPromises();
    expect(wrapper.text()).toContain('User does not exist');
    expect(wrapper.find('[data-test="ts2-users"]').findAll('.chip')).toHaveLength(1);
  });

  it('opens a slot and clears its users', async () => {
    const wrapper = await card();
    await radio(wrapper, 'ts2-lock', 'open').trigger('change');
    expect(wrapper.find('[data-test="ts2-users"]').exists()).toBe(false);
    await wrapper.find('[data-test="rules-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch.mock.calls[0][1]).toMatchObject({ ts2_users: [] });
  });

  it('switches to an allow list of talkgroups', async () => {
    const wrapper = await card();
    await radio(wrapper, 'tg-mode', 'allow').trigger('change');
    await wrapper.find('[data-test="add-talkgroup"]').setValue('3100');
    await wrapper.find('[data-test="talkgroups"] + form').trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="talkgroups"]').text()).toContain('USA');
    await wrapper.find('[data-test="add-talkgroup"]').setValue('77');
    await wrapper.find('[data-test="talkgroups"] + form').trigger('submit');
    await flushPromises();
    expect(wrapper.text()).toContain('No such talkgroup');
    await wrapper.find('[data-test="rules-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch.mock.calls[0][1]).toMatchObject({ talkgroup_mode: 'allow', talkgroups: [91, 3100] });
  });

  it('turns the talkgroup list off', async () => {
    const wrapper = await card();
    await radio(wrapper, 'tg-mode', 'off').trigger('change');
    expect(wrapper.find('[data-test="talkgroups"]').exists()).toBe(false);
    await wrapper.find('[data-test="rules-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch.mock.calls[0][1]).toMatchObject({ talkgroup_mode: 'off', talkgroups: [] });
  });

  it('refuses to save a talkgroup mode with no talkgroups', async () => {
    const wrapper = await card({ ...RULES, talkgroup_mode: 'off', talkgroups: [] });
    await radio(wrapper, 'tg-mode', 'block').trigger('change');
    await wrapper.find('[data-test="rules-save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="rules-message"]').text()).toBe('List at least one talkgroup');
  });

  it('resets the draft to the saved rules', async () => {
    const wrapper = await card();
    await radio(wrapper, 'ts2-lock', 'open').trigger('change');
    await wrapper.find('[data-test="rules-reset"]').trigger('click');
    expect(wrapper.find('[data-test="ts2-users"]').text()).toContain('KI5VMF');
  });

  it('shows the error of a failed save', async () => {
    const wrapper = await card();
    mockAPI.patch.mockRejectedValueOnce({ response: { status: 400, data: { error: 'User does not exist' } } });
    await wrapper.find('[data-test="rules-save"]').trigger('click');
    await flushPromises();
    expect(wrapper.find('[data-test="rules-message"]').text()).toBe('User does not exist');
  });
});
