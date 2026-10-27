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
  default: { post: vi.fn() },
}));
vi.mock('@unhead/vue', () => ({
  useHead: vi.fn(),
}));

import API from '@/services/API';
import VerifyEmailPage from '@/views/auth/VerifyEmailPage.vue';

const mockAPI = API as unknown as { post: Mock };

async function open(hash: string) {
  window.history.replaceState(null, '', '/verify-email' + hash);
  const wrapper = mount(VerifyEmailPage, { global: { stubs: { RouterLink: true } } });
  await flushPromises();
  return wrapper;
}

describe('VerifyEmailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('sends the token of the link and drops it from the address bar', async () => {
    mockAPI.post.mockResolvedValue({ data: { message: 'Your email address is confirmed.' } });
    const wrapper = await open('#token=abc123');
    expect(mockAPI.post).toHaveBeenCalledWith('/auth/verify-email', { token: 'abc123' });
    expect(wrapper.find('[data-test="verify-ok"]').text()).toBe('Your email address is confirmed.');
    expect(window.location.hash).toBe('');
  });

  it('says a used or old link does not work', async () => {
    mockAPI.post.mockRejectedValue({ response: { data: { error: 'This link does not work. It is older than 24 hours, was used already, or is for an older address.' } } });
    const wrapper = await open('#token=used');
    expect(wrapper.find('[data-test="verify-bad"]').text()).toContain('This link does not work.');
  });

  it('needs a token', async () => {
    const wrapper = await open('');
    expect(mockAPI.post).not.toHaveBeenCalled();
    expect(wrapper.find('[data-test="verify-bad"]').exists()).toBe(true);
  });
});
