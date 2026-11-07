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
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

vi.mock('@/components/ui/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import API from '@/services/API';
import { toast } from '@/components/ui/toast';
import UserTable from '@/components/UserTable.vue';
import { useUserStore } from '@/store';

const mockAPI = API as unknown as { get: Mock; post: Mock };
const mockToast = toast as unknown as { success: Mock; error: Mock };

const row = (id: number, callsign: string, admin = false) => ({
  id,
  callsign,
  username: callsign.toLowerCase(),
  approved: true,
  suspended: false,
  admin,
  superAdmin: false,
  repeaters: [],
  created_at: '2026-10-01T00:00:00Z',
});

async function mountTable() {
  mockAPI.get.mockResolvedValue({
    data: { total: 4, users: [row(1, 'XXXXXX'), row(1000001, 'N0CALL'), row(1000002, 'N1CALL'), row(1000003, 'N2CALL', true)] },
  });
  const wrapper = mount(UserTable, {
    attachTo: document.body,
    global: { mocks: { $toast: { add: vi.fn() }, $confirm: { require: vi.fn() } } },
  });
  await flushPromises();
  return wrapper;
}

function rowButtons(wrapper: ReturnType<typeof mount>, callsign: string) {
  const tr = wrapper.findAll('tr').find((r) => r.text().includes(callsign));
  expect(tr).toBeDefined();
  return tr!.findAll('button');
}

async function open(wrapper: ReturnType<typeof mount>, callsign: string) {
  const action = rowButtons(wrapper, callsign).find((b) => b.text() === 'Require password change');
  expect(action).toBeDefined();
  await action!.trigger('click');
  await flushPromises();
  const dialog = document.querySelector('[data-test="require-password-dialog"]') as HTMLElement | null;
  expect(dialog).not.toBeNull();
  return dialog!;
}

const dialog = () => document.querySelector('[data-test="require-password-dialog"]');
const click = async (sel: string) => {
  (document.querySelector(sel) as HTMLElement).click();
  await flushPromises();
};

describe('Require password change', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
    useUserStore().id = 1;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is a row action for other users, and for admins only to the System Admin', async () => {
    const wrapper = await mountTable();
    expect(rowButtons(wrapper, 'N0CALL').map((b) => b.text())).toEqual(['Edit', 'Require password change', 'Delete']);
    expect(rowButtons(wrapper, 'XXXXXX').map((b) => b.text())).toEqual(['Edit', 'Delete']);
    expect(rowButtons(wrapper, 'N2CALL').map((b) => b.text())).toEqual(['Edit', 'Delete']);

    useUserStore().superAdmin = true;
    await flushPromises();
    expect(rowButtons(wrapper, 'N2CALL').map((b) => b.text())).toEqual(['Edit', 'Require password change', 'Delete']);
  });

  it('asks first, with the random password off', async () => {
    const wrapper = await mountTable();
    const d = await open(wrapper, 'N0CALL');
    expect(d.textContent).toContain('Require a new password from N0CALL');
    expect(d.textContent).toContain(
      'DMRHub logs N0CALL out everywhere. They must choose a new password when they next log in.',
    );
    expect(d.querySelector('label[for="require-generate"]')?.textContent).toBe('Generate a random password');
    expect(d.querySelector('[data-test="require-generate"]')?.getAttribute('aria-checked')).toBe('false');
    expect(mockAPI.post).not.toHaveBeenCalled();
  });

  it('only sets the flag when the checkbox is off', async () => {
    mockAPI.post.mockResolvedValue({ data: { message: 'The user must choose a new password' } });
    const wrapper = await mountTable();
    await open(wrapper, 'N0CALL');
    await click('[data-test="require-confirm"]');
    expect(mockAPI.post).toHaveBeenCalledWith('/users/1000001/require-password-change', { generate_password: false });
    expect(mockToast.success).toHaveBeenCalledWith('N0CALL must choose a new password when they next log in.');
    expect(dialog()).toBeNull();
  });

  it('shows a generated password once and copies it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    mockAPI.post.mockResolvedValue({ data: { message: 'The user must choose a new password', password: 'Qm4tVx8rKp2Hw9Ns' } });
    const wrapper = await mountTable();
    await open(wrapper, 'N0CALL');
    await click('[data-test="require-generate"]');
    expect(dialog()?.textContent).toContain('Their current password stops working.');
    await click('[data-test="require-confirm"]');
    expect(mockAPI.post).toHaveBeenCalledWith('/users/1000001/require-password-change', { generate_password: true });
    const field = document.getElementById('require-password') as HTMLInputElement;
    expect(field.readOnly).toBe(true);
    expect(field.value).toBe('Qm4tVx8rKp2Hw9Ns');
    expect(dialog()?.textContent).toContain('It will not be shown again.');
    await click('[data-test="require-copy"]');
    expect(writeText).toHaveBeenCalledWith('Qm4tVx8rKp2Hw9Ns');
    expect(document.querySelector('[data-test="require-copied"]')?.textContent).toBe('Copied.');
    await click('[data-test="require-done"]');
    expect(dialog()).toBeNull();

    await open(wrapper, 'N1CALL');
    expect(document.getElementById('require-password')).toBeNull();
    expect(dialog()?.querySelector('[data-test="require-generate"]')?.getAttribute('aria-checked')).toBe('false');
  });

  it('shows the error of the server and keeps the dialog', async () => {
    mockAPI.post.mockRejectedValue({
      response: { status: 400, data: { error: 'Only the System Admin can reset the password of an admin.' } },
    });
    const wrapper = await mountTable();
    await open(wrapper, 'N0CALL');
    await click('[data-test="require-confirm"]');
    expect(mockToast.error).toHaveBeenCalledWith('Error', {
      description: 'Only the System Admin can reset the password of an admin.',
    });
    expect(mockToast.success).not.toHaveBeenCalled();
    expect(dialog()).not.toBeNull();
  });

  it('cancels without a change', async () => {
    const wrapper = await mountTable();
    await open(wrapper, 'N1CALL');
    await click('[data-test="require-cancel"]');
    expect(mockAPI.post).not.toHaveBeenCalled();
    expect(dialog()).toBeNull();
  });
});
