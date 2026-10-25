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
    patch: vi.fn(),
  },
}));

vi.mock('@/components/ui/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import API from '@/services/API';
import { toast } from '@/components/ui/toast';
import UserTable from '@/components/UserTable.vue';
import { useUserStore } from '@/store';
import { TEMPORARY_PASSWORD_ALPHABET } from '@/lib/password';

const mockAPI = API as unknown as { get: Mock; patch: Mock };
const mockToast = toast as unknown as { success: Mock; error: Mock };

const row = (id: number, callsign: string) => ({
  id,
  callsign,
  username: callsign.toLowerCase(),
  approved: true,
  suspended: false,
  admin: false,
  repeaters: [],
  created_at: '2026-10-01T00:00:00Z',
});

const temporary = new RegExp(`^[${TEMPORARY_PASSWORD_ALPHABET}]{16}$`);

async function mountTable() {
  mockAPI.get.mockResolvedValue({ data: { total: 3, users: [row(1, 'XXXXXX'), row(1000001, 'N0CALL'), row(1000002, 'N1CALL')] } });
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
  const reset = rowButtons(wrapper, callsign).find((b) => b.text() === 'Reset password');
  expect(reset).toBeDefined();
  await reset!.trigger('click');
  await flushPromises();
  const dialog = document.querySelector('[data-test="reset-password-dialog"]') as HTMLElement | null;
  expect(dialog).not.toBeNull();
  return dialog!;
}

const field = () => document.getElementById('reset-password') as HTMLInputElement;
const click = async (sel: string) => {
  (document.querySelector(sel) as HTMLElement).click();
  await flushPromises();
};

describe('Reset password', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    setActivePinia(createPinia());
    useUserStore().id = 1;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is a row action for other users, not for yourself', async () => {
    const wrapper = await mountTable();
    expect(rowButtons(wrapper, 'N0CALL').map((b) => b.text())).toEqual(['Edit', 'Reset password', 'Delete']);
    expect(rowButtons(wrapper, 'XXXXXX').map((b) => b.text())).toEqual(['Edit', 'Delete']);
  });

  it('opens the dialog of the mockup with a temporary password', async () => {
    const wrapper = await mountTable();
    const dialog = await open(wrapper, 'N0CALL');
    expect(dialog.textContent).toContain('Reset the password of N0CALL');
    expect(dialog.textContent).toContain(
      'DMRHub sets this temporary password and logs them out everywhere. Give it to them. They choose their own password when they log in with it.',
    );
    expect(dialog.querySelector('label[for="reset-password"]')?.textContent).toBe('Temporary password');
    expect(field().readOnly).toBe(true);
    expect(field().value).toMatch(temporary);
    const buttons = Array.from(dialog.querySelectorAll('button')).map((b) => b.textContent?.trim());
    expect(buttons).toEqual(['Copy', 'New', 'Set password', 'Cancel']);
  });

  it('makes a new password and copies it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    const wrapper = await mountTable();
    await open(wrapper, 'N0CALL');
    const first = field().value;
    await click('[data-test="reset-new"]');
    expect(field().value).toMatch(temporary);
    expect(field().value).not.toBe(first);
    await click('[data-test="reset-copy"]');
    expect(writeText).toHaveBeenCalledWith(field().value);
    expect(document.querySelector('[data-test="reset-copied"]')?.textContent).toBe('Copied.');
    await click('[data-test="reset-new"]');
    expect(document.querySelector('[data-test="reset-copied"]')).toBeNull();
  });

  it('sets the shown password and says so in a toast', async () => {
    mockAPI.patch.mockResolvedValue({ data: { message: 'User updated' } });
    const wrapper = await mountTable();
    await open(wrapper, 'N0CALL');
    const shown = field().value;
    await click('[data-test="reset-set"]');
    expect(mockAPI.patch).toHaveBeenCalledWith('/users/1000001', { password: shown });
    expect(mockToast.success).toHaveBeenCalledWith(
      "N0CALL's password is reset. They choose their own when they log in.",
    );
    expect(document.querySelector('[data-test="reset-password-dialog"]')).toBeNull();
  });

  it('shows the error of the server and keeps the dialog', async () => {
    mockAPI.patch.mockRejectedValue({
      response: { status: 400, data: { error: 'Only the System Admin can reset the password of an admin.' } },
    });
    const wrapper = await mountTable();
    await open(wrapper, 'N0CALL');
    await click('[data-test="reset-set"]');
    expect(mockToast.error).toHaveBeenCalledWith('Error', {
      description: 'Only the System Admin can reset the password of an admin.',
    });
    expect(mockToast.success).not.toHaveBeenCalled();
    expect(document.querySelector('[data-test="reset-password-dialog"]')).not.toBeNull();
  });

  it('cancels without a change', async () => {
    const wrapper = await mountTable();
    await open(wrapper, 'N1CALL');
    await click('[data-test="reset-cancel"]');
    expect(mockAPI.patch).not.toHaveBeenCalled();
    expect(document.querySelector('[data-test="reset-password-dialog"]')).toBeNull();
  });
});
