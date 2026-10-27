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
import type { RadioIDDrift } from '@/services/radioid';

vi.mock('@/services/API', () => ({
  default: { get: vi.fn(), post: vi.fn() },
}));

vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import RadioIDDriftPage from '@/views/admin/RadioIDDriftPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

function drift(radioID: number, over: Partial<RadioIDDrift> = {}): RadioIDDrift {
  return {
    radio_id: radioID,
    user_id: radioID,
    username: 'testuser',
    callsign: 'OLDCALL',
    radio_id_callsign: 'KI5VMF',
    found_at: '2026-10-20T01:00:00Z',
    checked_at: '2026-10-27T01:00:00Z',
    can_fix: true,
    can_notify: true,
    ...over,
  };
}

function serve(drifts: RadioIDDrift[]) {
  mockAPI.get.mockImplementation((url: string) => {
    if (url.startsWith('/admin/radio-id-drift?')) {
      return Promise.resolve({ data: { total: drifts.length, drifts } });
    }
    return Promise.reject(new Error(url));
  });
}

async function page() {
  const wrapper = mount(RadioIDDriftPage, {
    global: { plugins: [createPinia()], stubs: { RouterLink: { template: '<a><slot /></a>' } } },
    attachTo: document.body,
  });
  await flushPromises();
  return wrapper;
}

describe('RadioIDDriftPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('lists the radio IDs with a Fix only where one applies', async () => {
    serve([
      drift(3191868),
      drift(3125552, { username: 'n0gone', callsign: 'N0GONE', radio_id_callsign: '', can_fix: false }),
    ]);
    const wrapper = await page();
    const rows = wrapper.findAll('[data-test="drift-row"]');
    expect(rows).toHaveLength(2);
    expect(rows[0]!.text()).toContain('testuser');
    expect(rows[0]!.text()).toContain('3191868');
    expect(rows[0]!.text()).toContain('KI5VMF');
    expect(rows[0]!.find('[data-test="drift-fix"]').exists()).toBe(true);
    expect(rows[1]!.text()).toContain('Not listed');
    expect(rows[1]!.find('[data-test="drift-fix"]').exists()).toBe(false);
    expect(rows[1]!.find('[data-test="drift-edit"]').exists()).toBe(true);
  });

  it('says so when every callsign matches', async () => {
    serve([]);
    const wrapper = await page();
    expect(wrapper.find('[data-test="drift-empty"]').text()).toBe('Every callsign matches RadioID.');
  });

  it('fixes a radio ID and emails the user when asked', async () => {
    serve([drift(3191868)]);
    const wrapper = await page();
    await wrapper.find('[data-test="drift-fix"]').trigger('click');
    await flushPromises();
    expect(document.body.textContent).toContain('Change the callsign of testuser to KI5VMF?');
    const notify = document.querySelector('[data-test="drift-notify"]') as HTMLInputElement;
    expect(notify.checked).toBe(true);
    mockAPI.post.mockResolvedValue({ data: { message: 'User updated', callsign: 'KI5VMF', notified: true } });
    serve([]);
    (document.querySelector('[data-test="drift-confirm"]') as HTMLButtonElement).click();
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/admin/radio-id-drift/3191868/fix', { notify: true });
    expect(wrapper.find('[data-test="drift-ok"]').text()).toBe('testuser is now KI5VMF. Email sent.');
    expect(wrapper.find('[data-test="drift-empty"]').exists()).toBe(true);
  });

  it('offers no email when the user cannot get one', async () => {
    serve([drift(3191868, { can_notify: false })]);
    const wrapper = await page();
    await wrapper.find('[data-test="drift-fix"]').trigger('click');
    await flushPromises();
    expect(document.querySelector('[data-test="drift-notify"]')).toBeNull();
    mockAPI.post.mockResolvedValue({ data: { message: 'User updated', callsign: 'KI5VMF', notified: false } });
    (document.querySelector('[data-test="drift-confirm"]') as HTMLButtonElement).click();
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/admin/radio-id-drift/3191868/fix', { notify: false });
  });

  it('shows the server error of a fix', async () => {
    serve([drift(3191868)]);
    const wrapper = await page();
    await wrapper.find('[data-test="drift-fix"]').trigger('click');
    await flushPromises();
    mockAPI.post.mockRejectedValue({ response: { data: { error: 'Another user has the callsign KI5VMF' } } });
    (document.querySelector('[data-test="drift-confirm"]') as HTMLButtonElement).click();
    await flushPromises();
    expect(wrapper.find('[data-test="drift-error"]').text()).toBe('Another user has the callsign KI5VMF');
  });
});
