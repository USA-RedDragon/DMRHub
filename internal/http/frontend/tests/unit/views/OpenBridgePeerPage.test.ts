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

import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
}));
vi.mock('@/components/ui/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import API from '@/services/API';
import { keepPeerPassword } from '@/lib/peer';
import { useUserStore } from '@/store';
import OpenBridgePeerPage from '@/views/admin/OpenBridgePeerPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; patch: Mock; delete: Mock };

const peer = (enhanced: boolean) => ({
  id: 3000,
  ip: '192.0.2.10',
  port: 62081,
  ingress: true,
  egress: true,
  enhanced,
  codec2: false,
  last_ping_time: '0001-01-01T00:00:00Z',
  owner: { id: 3000001, callsign: 'N0CALL' },
});

const push = vi.fn();

async function mountPage(enhanced: boolean, admin = true) {
  const user = useUserStore();
  user.loggedIn = true;
  user.admin = admin;
  user.id = admin ? 1 : 3000001;
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/network/features') {
      return Promise.resolve({ data: { openbridge: true, openbridge_port: 62035, server_address: 'dmrhub.example.org' } });
    }
    return Promise.resolve({ data: peer(enhanced) });
  });
  const wrapper = mount(OpenBridgePeerPage, {
    attachTo: document.body,
    global: {
      mocks: { $route: { params: { id: '3000' } }, $router: { push } },
      stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } },
    },
  });
  await flushPromises();
  return wrapper;
}

const tabNames = (w: ReturnType<typeof mount>) => w.findAll('[role="tab"]').map((t) => t.text());
const click = async (sel: string) => {
  (document.querySelector(sel) as HTMLElement).click();
  await flushPromises();
};

describe('OpenBridgePeerPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
  });

  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('shows an enhanced peer only the tested FreeDMR steps', async () => {
    const w = await mountPage(true);
    expect(w.findAll('[role="tablist"]')).toHaveLength(0);
    expect(w.find('[data-test="setup-title"]').text()).toBe('Set up FreeDMR');
    const panel = w.find('[data-test="panel-freedmr"]').text();
    expect(panel).toContain('Tested');
    expect(panel).toContain('FreeDMR 546165e');
    expect(panel).toContain('SERVER_ID: 3000');
    expect(panel).toContain('NETWORK_ID: 3000');
    expect(panel).toContain('IP: 192.0.2.10');
    expect(panel).toContain('PORT: 62081');
    expect(panel).toContain('TARGET_IP: dmrhub.example.org');
    expect(panel).toContain('TARGET_PORT: 62035');
    expect(panel).toContain('ENHANCED_OBP: True');
    expect(panel).toContain('PROTO_VER: 5');
    expect(panel).toContain('PASSPHRASE: <peer password>');
    expect(w.text()).not.toContain('HBlink3');
    expect(w.text()).not.toContain('Other OpenBridge server');
    w.unmount();
  });

  it('shows any other peer HBlink3 and the other OpenBridge servers', async () => {
    const w = await mountPage(false);
    expect(tabNames(w)).toEqual(['HBlink3', 'Other OpenBridge server']);
    expect(w.find('[data-test="setup-title"]').text()).toBe('Set up the other server');
    const panel = w.find('[data-test="panel-hblink3"]').text();
    expect(panel).toContain('HBlink3 1db2730');
    expect(panel).toContain('[OBP-DMRHUB]');
    expect(panel).toContain('BOTH_SLOTS: True');
    expect(panel).toContain("UNIT = ['OBP-DMRHUB', 'LOCAL']");
    expect(panel).not.toContain('ENHANCED_OBP');
    expect(w.text()).not.toContain('FreeDMR 546165e');
    await w.find('[data-test="tab-other"]').trigger('mousedown', { button: 0 });
    await flushPromises();
    const other = w.find('[data-test="panel-other"]').text();
    expect(other).toContain('OpenBridge version 1');
    expect(other).toContain('dmrhub.example.org');
    expect(other).toContain('62035');
    w.unmount();
  });

  it('follows the enhanced box before it is saved', async () => {
    const w = await mountPage(false);
    await w.find('input#enhanced').setValue(true);
    expect(w.find('[data-test="setup-title"]').text()).toBe('Set up FreeDMR');
    await w.find('#ip').setValue('203.0.113.5');
    expect(w.find('[data-test="panel-freedmr"]').text()).toContain('IP: 203.0.113.5');
    w.unmount();
  });

  it('shows the connection details, and the password once right after the peer was added', async () => {
    keepPeerPassword(3000, 'Ab3dE6gH9jK2.');
    const first = await mountPage(true);
    expect(first.find('[data-test="title"]').text()).toBe('Peer 3000 is added');
    expect(first.find('[data-test="lead"]').text()).toBe('Owner N0CALL (3000001).');
    const details = first.find('[data-test="details"]').text();
    expect(details).toContain('dmrhub.example.org');
    expect(details).toContain('62035');
    expect(details).toContain('3000');
    expect(first.find('[data-test="secret"]').text()).toBe('Ab3dE6gH9jK2.');
    expect(first.find('[data-test="copy-now"]').text()).toContain('Copy the password now.');
    expect(first.find('[data-test="panel-freedmr"]').text()).toContain('PASSPHRASE: Ab3dE6gH9jK2.');
    expect(first.find('[data-test="rotate"]').exists()).toBe(false);
    expect(first.find('#owner').element).toHaveProperty('readOnly', true);
    expect(first.find('#pid').element).toHaveProperty('readOnly', true);
    first.unmount();

    const later = await mountPage(true);
    expect(later.find('[data-test="title"]').text()).toBe('Peer 3000');
    expect(later.find('[data-test="secret"]').exists()).toBe(false);
    expect(later.find('[data-test="copy-now"]').exists()).toBe(false);
    expect(later.find('[data-test="secret-hidden"]').text()).toBe('Hidden. DMRHub showed it once, when you added this peer.');
    later.unmount();
  });

  it('rotates the password only after the confirm, and shows the new one once', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'n3w-Peer_Pass', rotated_at: '2026-10-27T17:00:00Z' } });
    const w = await mountPage(false);
    await w.find('[data-test="rotate"]').trigger('click');
    await flushPromises();
    expect(document.querySelector('[data-test="rotate-dialog"]')).not.toBeNull();
    await click('[data-test="rotate-cancel"]');
    expect(mockAPI.post).not.toHaveBeenCalled();

    await w.find('[data-test="rotate"]').trigger('click');
    await flushPromises();
    await click('[data-test="rotate-confirm"]');
    expect(mockAPI.post).toHaveBeenCalledWith('/peers/3000/rotate-password', {});
    expect(w.find('[data-test="secret"]').text()).toBe('n3w-Peer_Pass');
    expect(w.find('[data-test="rotated"]').text()).toContain('The old password stopped working.');
    expect(w.find('[data-test="panel-hblink3"]').text()).toContain('PASSPHRASE: n3w-Peer_Pass');
    expect(w.find('[data-test="title"]').text()).toBe('Peer 3000');
    w.unmount();
  });

  it('saves the fields with PATCH, with the admin-only ones only for an admin', async () => {
    mockAPI.patch.mockResolvedValue({ data: { ...peer(false), ip: '192.0.2.20', port: 62090 } });
    const w = await mountPage(false);
    await w.find('#ip').setValue('192.0.2.20');
    await w.find('#port').setValue('62090');
    await w.find('[data-test="save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/3000', {
      ip: '192.0.2.20', port: 62090, ingress: true, egress: true, enhanced: false, codec2: false,
    });
    w.unmount();

    mockAPI.patch.mockClear();
    mockAPI.patch.mockResolvedValue({ data: peer(false) });
    const owner = await mountPage(false, false);
    expect((owner.find('input#enhanced').element as HTMLInputElement).disabled).toBe(true);
    expect((owner.find('input#codec2').element as HTMLInputElement).disabled).toBe(true);
    expect(owner.find('[data-test="crumb"]').text()).not.toContain('Admin');
    await owner.find('[data-test="save"]').trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/3000', { ip: '192.0.2.10', port: 62081, ingress: true, egress: true });
    owner.unmount();
  });

  it('deletes the peer only after the confirm, and goes back to the peers', async () => {
    mockAPI.delete.mockResolvedValue({ data: { message: 'Peer deleted' } });
    const w = await mountPage(false);
    await w.find('[data-test="delete"]').trigger('click');
    await flushPromises();
    await click('[data-test="delete-cancel"]');
    expect(mockAPI.delete).not.toHaveBeenCalled();
    await w.find('[data-test="delete"]').trigger('click');
    await flushPromises();
    await click('[data-test="delete-confirm"]');
    expect(mockAPI.delete).toHaveBeenCalledWith('/peers/3000');
    expect(push).toHaveBeenCalledWith('/admin/peers');
    w.unmount();
  });

  it('links the rules section of the guide', async () => {
    const w = await mountPage(false);
    expect(w.find('[data-test="next-rules"]').attributes('href')).toBe(
      'https://github.com/USA-RedDragon/DMRHub/blob/main/doc/admin/openbridge.md#add-the-rules-of-a-peer',
    );
    w.unmount();
  });
});
