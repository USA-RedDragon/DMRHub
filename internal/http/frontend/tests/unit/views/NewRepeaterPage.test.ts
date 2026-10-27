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
import { createPinia } from 'pinia';

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

import API from '@/services/API';
import NewRepeaterPage from '@/views/repeaters/NewRepeaterPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

async function mountPage(features: Record<string, unknown>) {
  mockAPI.get.mockResolvedValue({ data: features });
  const toast = { add: vi.fn() };
  const wrapper = mount(NewRepeaterPage, {
    global: {
      plugins: [createPinia()],
      mocks: { $toast: toast, $router: { push: vi.fn() } },
    },
  });
  await flushPromises();
  return { wrapper, toast };
}

describe('NewRepeaterPage', () => {
  let alert: Mock;

  beforeEach(() => {
    vi.clearAllMocks();
    alert = vi.fn();
    vi.spyOn(window, 'alert').mockImplementation(alert);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows the configured MMDVM port in the DMRGateway snippet', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'secret' } });
    const { wrapper } = await mountPage({ ipsc: false, mmdvm_port: 62040, ipsc_port: 50010 });
    await wrapper.find('input#radioID').setValue('310000101');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(alert).toHaveBeenCalledTimes(1);
    const text = alert.mock.calls[0]![0] as string;
    expect(text).toContain('Port=62040\n');
    expect(text).not.toContain('62031');
  });

  it('shows the configured IPSC port with the auth key', async () => {
    mockAPI.post.mockResolvedValue({ data: { password: 'abcd' } });
    const { wrapper } = await mountPage({ ipsc: true, mmdvm_port: 62040, ipsc_port: 50010 });
    await wrapper.find('select#repeaterType').setValue('ipsc');
    await wrapper.find('input#radioID').setValue('310001');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    const text = alert.mock.calls[0]![0] as string;
    expect(text).toContain('Port: 50010\n');
    expect(text).not.toContain('50000');
  });

  it('says the repeater could not be created when the API refuses it', async () => {
    mockAPI.post.mockRejectedValue(new Error('network'));
    const { wrapper, toast } = await mountPage({ ipsc: false, mmdvm_port: 62031, ipsc_port: 50000 });
    await wrapper.find('input#radioID').setValue('310000101');
    await wrapper.find('form').trigger('submit');
    await flushPromises();
    expect(toast.add).toHaveBeenCalledWith(expect.objectContaining({ detail: 'Error creating repeater' }));
  });
});
