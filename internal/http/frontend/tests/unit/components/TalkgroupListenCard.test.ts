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
  default: { patch: vi.fn() },
}));

import API from '@/services/API';
import TalkgroupListenCard from '@/components/TalkgroupListenCard.vue';

const mockAPI = API as unknown as { patch: Mock };

describe('TalkgroupListenCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('takes the talkgroup off the monitor and puts it back', async () => {
    mockAPI.patch.mockResolvedValue({ data: {} });
    const w = mount(TalkgroupListenCard, { props: { talkgroupId: 3100, noListen: false } });
    const sw = () => w.find('[data-test="tg-listen"]');
    expect(sw().attributes('aria-checked')).toBe('true');

    await sw().trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenLastCalledWith('/talkgroups/3100', { listen: false });
    expect(sw().attributes('aria-checked')).toBe('false');

    await sw().trigger('click');
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenLastCalledWith('/talkgroups/3100', { listen: true });
    expect(sw().attributes('aria-checked')).toBe('true');
  });

  it('keeps the switch and shows why when the save fails', async () => {
    mockAPI.patch.mockRejectedValue(new Error('down'));
    const w = mount(TalkgroupListenCard, { props: { talkgroupId: 3100, noListen: true } });
    await w.find('[data-test="tg-listen"]').trigger('click');
    await flushPromises();
    expect(w.find('[data-test="tg-listen"]').attributes('aria-checked')).toBe('false');
    expect(w.find('[data-test="tg-listen-message"]').exists()).toBe(true);
  });
});
