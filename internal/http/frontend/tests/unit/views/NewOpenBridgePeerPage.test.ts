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

vi.mock('@/services/API', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

import API from '@/services/API';
import NewOpenBridgePeerPage from '@/views/admin/NewOpenBridgePeerPage.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock };

const OFF = 'Off: this peer gets every call as AMBE. Codec2 calls are converted before they go to it.';
const ON = 'On: this peer gets every call in the codec it was sent in. Turn this on only when every network behind the peer can carry Codec2.';

async function mountPage() {
  mockAPI.get.mockResolvedValue({ data: { users: [{ id: 1000001, callsign: 'N0CALL' }] } });
  const wrapper = mount(NewOpenBridgePeerPage, {
    global: {
      mocks: { $toast: { add: vi.fn() }, $router: { push: vi.fn() } },
    },
  });
  await flushPromises();
  return wrapper;
}

describe('NewOpenBridgePeerPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(window, 'alert').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('has Codec2 off by default and words both states', async () => {
    const wrapper = await mountPage();
    const box = wrapper.find('input#codec2');
    expect((box.element as HTMLInputElement).checked).toBe(false);
    expect(wrapper.find('label[for="codec2"]').text()).toBe('This peer carries Codec2');
    expect(wrapper.text()).toContain(OFF);
    expect(wrapper.text()).not.toContain(ON);
    await box.setValue(true);
    expect(wrapper.text()).toContain(ON);
    expect(wrapper.text()).not.toContain(OFF);
  });

  it('places the Codec2 setting after the enhanced setting', async () => {
    const wrapper = await mountPage();
    const ids = wrapper.findAll('input[type="checkbox"]').map((e) => e.attributes('id'));
    expect(ids).toEqual(['ingress', 'egress', 'enhanced', 'codec2']);
  });

  for (const codec2 of [false, true]) {
    it(`sends codec2 ${codec2} with the new peer`, async () => {
      mockAPI.post.mockResolvedValue({ data: { password: 'secret' } });
      const wrapper = await mountPage();
      await wrapper.find('select#owner').setValue('1000001');
      await wrapper.find('#id').setValue('311999');
      await wrapper.find('#ip').setValue('203.0.113.20');
      await wrapper.find('#port').setValue('62044');
      if (codec2) {
        await wrapper.find('input#codec2').setValue(true);
      }
      await wrapper.find('form').trigger('submit');
      await flushPromises();
      expect(mockAPI.post).toHaveBeenCalledWith('/peers', expect.objectContaining({ id: 311999, enhanced: false, codec2 }));
    });
  }
});
