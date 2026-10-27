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
  default: { get: vi.fn(), post: vi.fn() },
}));
vi.mock('@/components/ui/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import API from '@/services/API';
import { keepSecret } from '@/lib/enroll';
import { useUserStore } from '@/store';
import RepeaterConnectPage from '@/views/repeaters/RepeaterConnectPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

const owner = { id: 3000001, callsign: 'N0CALL' };
const never = '0001-01-01T00:00:00Z';

type Fixture = { id: number; type: string; lastPing?: string; ownerId?: number };

async function mountPage(f: Fixture) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url === '/network/features') {
      return Promise.resolve({ data: { ipsc: true, mmdvm_port: 62040, ipsc_port: 50010, server_address: 'dmrhub.example.org' } });
    }
    return Promise.resolve({
      data: {
        id: f.id,
        type: f.type,
        connected_time: f.lastPing ?? never,
        last_ping_time: f.lastPing ?? never,
        owner: { ...owner, id: f.ownerId ?? owner.id },
      },
    });
  });
  const wrapper = mount(RepeaterConnectPage, {
    attachTo: document.body,
    global: {
      mocks: { $route: { params: { id: String(f.id) } } },
      stubs: { RouterLink: { template: '<a><slot /></a>' } },
    },
  });
  await flushPromises();
  return wrapper;
}

const tabNames = (w: ReturnType<typeof mount>) => w.findAll('[role="tab"]').map((t) => t.text());
const dialog = () => document.querySelector('[data-test="rotate-dialog"]');
const click = async (sel: string) => {
  (document.querySelector(sel) as HTMLElement).click();
  await flushPromises();
};

describe('RepeaterConnectPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
    const user = useUserStore();
    user.loggedIn = true;
    user.id = owner.id;
  });

  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('shows a hotspot only the MMDVM port and the MMDVM clients', async () => {
    const w = await mountPage({ id: 300000101, type: 'mmdvm' });
    expect(tabNames(w)).toEqual(['Pi-Star', 'WPSD', 'MMDVMHost', 'DMRGateway', 'DroidStar', 'HBlink3', 'Other']);
    const details = w.find('[data-test="details"]').text();
    expect(details).toContain('Server address');
    expect(details).toContain('dmrhub.example.org');
    expect(details).toContain('62040');
    expect(details).toContain('Hotspot ID');
    expect(details).toContain('Password');
    expect(w.text()).not.toContain('50010');
    expect(w.text()).not.toContain('Auth key');
    expect(w.text()).not.toContain('Motorola');
    w.unmount();
  });

  it('shows an IPSC repeater only the IPSC port, the auth key and its clients', async () => {
    const w = await mountPage({ id: 311860, type: 'ipsc' });
    expect(tabNames(w)).toEqual(['Motorola CPS', 'Other']);
    const details = w.find('[data-test="details"]').text();
    expect(details).toContain('50010');
    expect(details).toContain('Repeater ID');
    expect(details).toContain('Auth key');
    expect(w.text()).not.toContain('62040');
    expect(w.text()).not.toContain('Pi-Star');
    expect(w.find('[data-test="rotate"]').text()).toBe('Rotate auth key');
    expect(w.find('[data-test="panel-cps"]').text()).toContain('Not tested');
    await w.find('[data-test="tab-ipsc-other"]').trigger('mousedown', { button: 0 });
    await flushPromises();
    const other = w.find('[data-test="panel-ipsc-other"]').text();
    expect(other).toContain('Motorola IPSC');
    expect(other).not.toContain('Options');
    expect(other).not.toContain('MMDVM');
    w.unmount();
  });

  it('shows the secret of a new hotspot once, and hides it on the next visit', async () => {
    keepSecret(300000101, 'Ab3dE6gH9jK2');
    const first = await mountPage({ id: 300000101, type: 'mmdvm' });
    expect(first.find('[data-test="title"]').text()).toBe('Hotspot 300000101 is enrolled');
    expect(first.find('[data-test="secret"]').text()).toBe('Ab3dE6gH9jK2');
    expect(first.find('[data-test="copy-now"]').text()).toContain('Copy the password now. This page shows it only once.');
    expect(first.find('[data-test="copy-now"]').text()).toContain('Rotate password');
    expect(first.find('[data-test="panel-pistar"]').text()).toContain('Ab3dE6gH9jK2');
    expect(first.find('[data-test="rotate"]').exists()).toBe(false);
    first.unmount();

    const later = await mountPage({ id: 300000101, type: 'mmdvm' });
    expect(later.find('[data-test="title"]').text()).toBe('Hotspot 300000101: connect');
    expect(later.find('[data-test="secret"]').exists()).toBe(false);
    expect(later.find('[data-test="copy-now"]').exists()).toBe(false);
    expect(later.find('[data-test="secret-hidden"]').text()).toBe('Hidden');
    expect(later.text()).not.toContain('Ab3dE6gH9jK2');
    expect(later.find('[data-test="panel-pistar"]').text()).toContain('<hotspot password>');
    expect(later.find('[data-test="rotate"]').text()).toBe('Rotate password');
    later.unmount();
  });

  it('closes the rotate dialog on Cancel and on Escape without a rotation', async () => {
    const w = await mountPage({ id: 300000101, type: 'mmdvm' });
    await w.find('[data-test="rotate"]').trigger('click');
    await flushPromises();
    expect(dialog()?.textContent).toContain('Rotate the password of 300000101?');
    expect(dialog()?.textContent).toContain('The hotspot disconnects at once.');
    await click('[data-test="rotate-cancel"]');
    expect(dialog()).toBeNull();

    await w.find('[data-test="rotate"]').trigger('click');
    await flushPromises();
    expect(dialog()).not.toBeNull();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await flushPromises();
    expect(dialog()).toBeNull();
    expect(mockAPI.post).not.toHaveBeenCalled();
    expect(w.find('[data-test="secret"]').exists()).toBe(false);
    w.unmount();
  });

  it('rotates on confirm and shows the new secret once', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'Qm7rT4vW8yZ2', rotated_at: new Date().toISOString() } });
    const w = await mountPage({ id: 300000101, type: 'mmdvm', lastPing: new Date(Date.now() - 3000).toISOString() });
    expect(w.find('[data-test="live"]').text()).toBe('Connected now. Last ping 3 seconds ago');
    await w.find('[data-test="rotate"]').trigger('click');
    await flushPromises();
    await click('[data-test="rotate-confirm"]');
    expect(mockAPI.post).toHaveBeenCalledWith('/repeaters/300000101/rotate-password', {});
    expect(dialog()).toBeNull();
    expect(w.find('[data-test="secret"]').text()).toBe('Qm7rT4vW8yZ2');
    expect(w.find('[data-test="rotated"]').text()).toContain('The old password stopped working.');
    expect(w.find('[data-test="live"]').text()).toBe('Waiting for a login with the new password');
    w.unmount();
  });

  it('offers no rotation to a user who does not own the repeater', async () => {
    const w = await mountPage({ id: 300000101, type: 'mmdvm', ownerId: 3000002 });
    expect(w.find('[data-test="secret-hidden"]').exists()).toBe(true);
    expect(w.find('[data-test="rotate"]').exists()).toBe(false);
    w.unmount();
  });

  it('fills the DMRGateway section with the picked network number', async () => {
    keepSecret(300000101, 'Ab3dE6gH9jK2');
    const w = await mountPage({ id: 300000101, type: 'mmdvm' });
    await w.find('[data-test="tab-dmrgateway"]').trigger('mousedown', { button: 0 });
    await flushPromises();
    await w.find('[data-test="dg-network"]').setValue('3');
    const ini = w.find('[data-test="panel-dmrgateway"] pre').text();
    expect(ini).toContain('[DMR Network 3]\nEnabled=1\nName=DMRHub\nAddress=dmrhub.example.org\nPort=62040\nId=300000101\nPassword="Ab3dE6gH9jK2"');
    expect(w.text()).not.toContain('AREDN');
    w.unmount();
  });
});
