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

import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}));

import API from '@/services/API';
import TokensCard from '@/components/users/TokensCard.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; delete: Mock };

const scopes = [
  { name: 'talkgroups:read', label: 'talkgroups', description: 'See the talkgroups of this network' },
  { name: 'lastheard:read', label: 'last heard', description: 'See last heard for your radio IDs and your hotspots' },
  { name: 'messages:read', label: 'messages', description: 'Read your text messages, and get new ones as they arrive' },
];

const homeAssistant = {
  id: 't1',
  name: 'Home Assistant',
  prefix: 'dmrhub_pat_k9Vd',
  scopes: ['lastheard:read'],
  full: false,
  can_use: 'Last heard',
  days: 0,
  made: '2026-10-12T12:00:00Z',
  last_used: '2026-11-01T14:12:00Z',
  expires: null,
  expired: false,
};
const backup = {
  id: 't2',
  name: 'Backup job',
  prefix: 'dmrhub_pat_3xQe',
  scopes: [],
  full: true,
  can_use: 'Full account access',
  days: 365,
  made: '2026-09-20T12:00:00Z',
  last_used: null,
  expires: '2027-09-20T12:00:00Z',
  expired: false,
};
const hotspot = {
  id: 't3',
  name: 'Hotspot script',
  prefix: 'dmrhub_pat_Pm2s',
  scopes: ['hotspots:talkgroups'],
  full: false,
  can_use: 'Hotspot talkgroups',
  days: 90,
  made: '2026-08-03T12:00:00Z',
  last_used: '2026-10-02T23:40:00Z',
  expires: '2026-11-01T12:00:00Z',
  expired: true,
};

function list(tokens = [homeAssistant, backup, hotspot]) {
  return { data: { tokens, scopes, expiries: [30, 90, 365, 0] } };
}

async function mountCard() {
  const wrapper = mount(TokensCard, { attachTo: document.body });
  await flushPromises();
  return wrapper;
}

describe('TokensCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('lists the tokens with what each can use and when it expires', async () => {
    mockAPI.get.mockResolvedValue(list());
    const wrapper = await mountCard();
    expect(mockAPI.get).toHaveBeenCalledWith('/users/me/tokens');
    expect(wrapper.text()).toContain('For your own scripts. A token acts as you. A password reset deletes every token.');
    expect(wrapper.findAll('th').map((th) => th.text())).toEqual(['Name', 'Can use', 'Made', 'Last used', 'Expires', 'Actions']);
    const rows = wrapper.findAll('tbody tr');
    expect(rows).toHaveLength(3);
    expect(rows[0].text()).toContain('Home Assistant');
    expect(rows[0].text()).toContain('dmrhub_pat_k9Vd');
    expect(rows[0].text()).toContain('Last heard');
    expect(rows[0].text()).toContain('Oct 12, 2026');
    expect(rows[0].find('[data-test="token-expires"]').text()).toBe('Never');
    expect(rows[1].find('[data-test="token-full"]').text()).toBe('Full account access');
    expect(rows[1].find('[data-test="token-last-used"]').text()).toBe('Never');
    expect(rows[1].find('[data-test="token-expires"]').text()).toBe('Sep 20, 2027');
    expect(rows[2].find('[data-test="token-expires"]').text()).toBe('Expired Nov 1, 2026');
    for (const row of rows) {
      expect(row.findAll('button').map((b) => b.text())).toEqual(['Rotate', 'Delete']);
    }
    expect(wrapper.find('[data-test="token-form"]').exists()).toBe(false);
  });

  it('says when there are no tokens', async () => {
    mockAPI.get.mockResolvedValue(list([]));
    const wrapper = await mountCard();
    expect(wrapper.text()).toContain('No tokens yet.');
    expect(wrapper.find('table').exists()).toBe(false);
  });

  it('makes a token that can use only what the user picks, and shows it once', async () => {
    mockAPI.get.mockResolvedValue(list([homeAssistant]));
    const made = { ...homeAssistant, id: 't9', name: 'Net logger', prefix: 'dmrhub_pat_7Hq2', scopes: ['talkgroups:read', 'lastheard:read'], can_use: 'Talkgroups, last heard', last_used: null, days: 90, expires: '2027-01-30T12:00:00Z' };
    mockAPI.post.mockResolvedValue({ data: { token: made, secret: 'dmrhub_pat_7Hq2mV0cXk9sR4tB1nLwE8yZ3aP6uJdFq0Wc5nRb2Ls' } });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="new-token"]').trigger('click');
    const form = wrapper.find('[data-test="token-form"]');
    expect(form.exists()).toBe(true);
    expect(form.text()).toContain('See the talkgroups of this network');
    expect(form.text()).not.toContain('can do everything you can do signed in');
    const expiry = form.find('select');
    expect(expiry.findAll('option').map((o) => o.text())).toEqual(['In 30 days', 'In 90 days', 'In 1 year', 'Never']);
    expect((expiry.element as HTMLSelectElement).value).toBe('90');

    await form.find('#token-name').setValue('Net logger');
    await form.find('input[value="talkgroups:read"]').setValue(true);
    await form.find('input[value="lastheard:read"]').setValue(true);
    await form.trigger('submit');
    await flushPromises();

    expect(mockAPI.post).toHaveBeenCalledWith('/users/me/tokens', {
      name: 'Net logger',
      scopes: ['talkgroups:read', 'lastheard:read'],
      full: false,
      expires_in_days: 90,
    });
    const once = wrapper.find('[data-test="token-secret"]');
    expect(once.text()).toContain('Copy the token now');
    expect(once.text()).toContain('DMRHub shows it only once.');
    expect((once.find('input').element as HTMLInputElement).value).toBe('dmrhub_pat_7Hq2mV0cXk9sR4tB1nLwE8yZ3aP6uJdFq0Wc5nRb2Ls');
    expect(wrapper.find('[data-test="token-form"]').exists()).toBe(false);
    expect(wrapper.findAll('tbody tr')).toHaveLength(2);
    expect(wrapper.find('[data-test="token-t9"]').text()).toContain('Net logger');
  });

  it('says what full account access can and cannot do, and makes such a token', async () => {
    mockAPI.get.mockResolvedValue(list([]));
    mockAPI.post.mockResolvedValue({ data: { token: backup, secret: 'dmrhub_pat_3xQe' + 'a'.repeat(39) } });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="new-token"]').trigger('click');
    const form = wrapper.find('[data-test="token-form"]');
    await form.find('input[value="full"]').setValue(true);
    expect(form.find('input[value="talkgroups:read"]').exists()).toBe(false);
    expect(form.find('[data-test="full-access-note"]').text()).toBe(
      'Full account access can do everything you can do signed in. It cannot open admin pages, also when you are an admin, ' +
        'and it cannot change account security: tokens, apps, password, username, callsign, email, or deleting the account.',
    );
    await form.find('#token-name').setValue('Backup job');
    await form.find('select').setValue('0');
    await form.trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/users/me/tokens', { name: 'Backup job', scopes: [], full: true, expires_in_days: 0 });
  });

  it('asks for a name and something to use before it sends', async () => {
    mockAPI.get.mockResolvedValue(list([]));
    const wrapper = await mountCard();
    await wrapper.find('[data-test="new-token"]').trigger('click');
    const form = wrapper.find('[data-test="token-form"]');
    await form.trigger('submit');
    expect(wrapper.find('[data-test="token-error"]').text()).toBe('Enter a name.');
    await form.find('#token-name').setValue('x');
    await form.trigger('submit');
    expect(wrapper.find('[data-test="token-error"]').text()).toBe('Pick at least one thing the token can use, or full account access.');
    expect(mockAPI.post).not.toHaveBeenCalled();
  });

  it('shows the error of the server', async () => {
    mockAPI.get.mockResolvedValue(list([]));
    mockAPI.post.mockRejectedValue({ response: { status: 400, data: { error: 'You have the most tokens you can have. Delete one first.' } } });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="new-token"]').trigger('click');
    const form = wrapper.find('[data-test="token-form"]');
    await form.find('#token-name').setValue('x');
    await form.find('input[value="full"]').setValue(true);
    await form.trigger('submit');
    await flushPromises();
    expect(wrapper.find('[data-test="token-error"]').text()).toBe('You have the most tokens you can have. Delete one first.');
    expect(wrapper.find('[data-test="token-secret"]').exists()).toBe(false);
  });

  it('rotates a token and shows the new secret once', async () => {
    mockAPI.get.mockResolvedValue(list());
    const rotated = { ...homeAssistant, id: 't7', prefix: 'dmrhub_pat_Zc4k', made: '2026-11-01T20:00:00Z', last_used: null };
    mockAPI.post.mockResolvedValue({ data: { token: rotated, secret: 'dmrhub_pat_Zc4kP1wQ8sN3vB6mT0rLx7yE2uH9aJdGf5Kq' } });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="token-t1"] [data-test="rotate"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/users/me/tokens/t1/rotate');
    const once = wrapper.find('[data-test="token-secret"]');
    expect(once.text()).toContain('Copy the new token of Home Assistant now');
    expect(once.text()).toContain('The old token stopped working. DMRHub shows the new one only once.');
    expect((once.find('input').element as HTMLInputElement).value).toBe('dmrhub_pat_Zc4kP1wQ8sN3vB6mT0rLx7yE2uH9aJdGf5Kq');
    expect(wrapper.find('[data-test="token-t1"]').exists()).toBe(false);
    expect(wrapper.findAll('tbody tr')[0].attributes('data-test')).toBe('token-t7');
    expect(wrapper.findAll('tbody tr')).toHaveLength(3);
  });

  it('deletes a token and says it stopped working', async () => {
    mockAPI.get.mockResolvedValue(list());
    mockAPI.delete.mockResolvedValue({ data: {} });
    const wrapper = await mountCard();
    await wrapper.find('[data-test="token-t2"] [data-test="delete"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/users/me/tokens/t2');
    expect(wrapper.findAll('tbody tr')).toHaveLength(2);
    expect(wrapper.text()).toContain('Deleted Backup job. It stopped working.');
  });
});
