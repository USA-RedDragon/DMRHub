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
  default: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

import API from '@/services/API';
import CosysopsCard from '@/components/CosysopsCard.vue';
import CosysopInvites from '@/components/CosysopInvites.vue';
import type { Cosysops } from '@/lib/cosysops';

const mockAPI = API as unknown as { get: Mock; post: Mock; delete: Mock };

const accepted = {
  id: 3191869, callsign: 'KI5VMF', username: 'kevin', invited_at: '2026-10-30T12:00:00Z',
  accepted_at: '2026-10-30T13:00:00Z', expires_at: null, pending: false,
};
const pending = {
  id: 3191870, callsign: 'KQ4APS', username: 'ana', invited_at: '2026-11-01T12:00:00Z',
  accepted_at: null, expires_at: '2026-11-08T12:00:00Z', pending: true,
};

function card(data: Cosysops, myId: number) {
  return mount(CosysopsCard, { props: { repeaterId: 319186801, data, myId } });
}

describe('CosysopsCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows the owner the co-sysops, the open invites and the invite form', () => {
    const w = card({ owner: { id: 3191868, callsign: 'KP4DJT' }, role: 'owner', cosysops: [accepted, pending] }, 3191868);
    expect(w.text()).toContain('KI5VMF');
    expect(w.text()).toContain('Oct 30, 2026');
    expect(w.find('[data-test="cosysop-3191870"]').text()).toContain('Invited, answer by Nov 8, 2026');
    expect(w.find('[data-test="remove-3191869"]').text()).toBe('Remove');
    expect(w.find('[data-test="remove-3191870"]').text()).toBe('Cancel invite');
    expect(w.find('[data-test="invite-user"]').exists()).toBe(true);
  });

  it('invites by callsign or username and asks for a reload', async () => {
    mockAPI.post.mockResolvedValue({ data: { ...pending } });
    const w = card({ owner: { id: 3191868, callsign: 'KP4DJT' }, role: 'owner', cosysops: [] }, 3191868);
    expect(w.text()).toContain('No co-sysops yet.');
    await w.find('[data-test="invite-user"]').setValue(' ana ');
    await w.find('form').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/319186801/cosysops', { user: 'ana' });
    expect(w.emitted('changed')).toHaveLength(1);
    expect(w.find('[data-test="cosysops-message"]').text()).toContain('Invited KQ4APS');
  });

  it('shows the error of a refused invite', async () => {
    mockAPI.post.mockRejectedValue({ response: { data: { error: 'No user has that callsign or username' } } });
    const w = card({ owner: { id: 3191868, callsign: 'KP4DJT' }, role: 'owner', cosysops: [] }, 3191868);
    await w.find('[data-test="invite-user"]').setValue('nobody');
    await w.find('form').trigger('submit');
    await flushPromises();
    expect(w.find('[data-test="cosysops-message"]').text()).toBe('No user has that callsign or username');
    expect(w.emitted('changed')).toBeUndefined();
  });

  it('shows a co-sysop the owner and only a Leave button on their own row', async () => {
    mockAPI.delete.mockResolvedValue({ data: {} });
    const other = { ...accepted, id: 3191871, callsign: 'W5DFW', username: 'w5dfw' };
    const w = card({ owner: { id: 3191868, callsign: 'KP4DJT' }, role: 'cosysop', cosysops: [accepted, other] }, 3191869);
    expect(w.text()).toContain('Owner: KP4DJT.');
    expect(w.find('[data-test="invite-user"]').exists()).toBe(false);
    expect(w.find('[data-test="remove-3191871"]').exists()).toBe(false);
    await w.find('[data-test="remove-3191869"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/repeaters/319186801/cosysops/3191869');
    expect(w.emitted('changed')).toHaveLength(1);
  });
});

describe('CosysopInvites', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const invite = {
    repeater_id: 311888, repeater_callsign: 'W5DFW', owner: { id: 3191868, callsign: 'KP4DJT' },
    invited_at: '2026-11-01T12:00:00Z', expires_at: '2026-11-08T12:00:00Z',
  };

  it('shows nothing without invites', async () => {
    mockAPI.get.mockResolvedValue({ data: { invites: [] } });
    const w = mount(CosysopInvites);
    await flushPromises();
    expect(w.find('[data-test="cosysop-invites"]').exists()).toBe(false);
  });

  it('accepts an invite and asks for the repeaters again', async () => {
    mockAPI.get.mockResolvedValue({ data: { invites: [invite] } });
    mockAPI.post.mockResolvedValue({ data: {} });
    const w = mount(CosysopInvites);
    await flushPromises();
    expect(w.text()).toContain('KP4DJT invited you to manage W5DFW 311888.');
    expect(w.text()).toContain('Answer by Nov 8, 2026.');
    await w.find('[data-test="accept-311888"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/311888/cosysop-invite/accept');
    expect(w.emitted('accepted')).toHaveLength(1);
    expect(w.find('[data-test="cosysop-invites"]').exists()).toBe(false);
  });

  it('declines an invite', async () => {
    mockAPI.get.mockResolvedValue({ data: { invites: [invite] } });
    mockAPI.post.mockResolvedValue({ data: {} });
    const w = mount(CosysopInvites);
    await flushPromises();
    await w.find('[data-test="decline-311888"]').trigger('click');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/311888/cosysop-invite/decline');
    expect(w.emitted('accepted')).toBeUndefined();
  });
});
