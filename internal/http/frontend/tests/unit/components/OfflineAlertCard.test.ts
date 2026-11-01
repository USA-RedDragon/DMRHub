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
  default: { get: vi.fn(), put: vi.fn() },
}));

import API from '@/services/API';
import OfflineAlertCard from '@/components/OfflineAlertCard.vue';

const mockAPI = API as unknown as { get: Mock; put: Mock };

const state = (extra: Record<string, unknown> = {}) => ({
  smtp: true, email_verified: true, email: 'k***@example.org', enabled: false, delay_minutes: 10, back: false,
  delays: [5, 10, 30, 60], ...extra,
});

function card() {
  return mount(OfflineAlertCard, { props: { repeaterId: 319186801, device: 'hotspot' }, global: { stubs: { RouterLink: true } } });
}

describe('OfflineAlertCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows nothing when the network sends no email', async () => {
    mockAPI.get.mockResolvedValue({ data: state({ smtp: false }) });
    const w = card();
    await flushPromises();
    expect(w.find('[data-test="offline-alert"]').exists()).toBe(false);
  });

  it('turns the alert on and then sets the delay and the back email', async () => {
    mockAPI.get.mockResolvedValue({ data: state() });
    mockAPI.put.mockImplementation((_url: string, body: Record<string, unknown>) => Promise.resolve({ data: state(body) }));
    const w = card();
    await flushPromises();
    expect(w.text()).toContain('Get an email when this hotspot stops talking to DMRHub.');
    expect(w.find('[data-test="alert-delay"]').exists()).toBe(false);

    await w.find('[data-test="alert-on"]').trigger('click');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenLastCalledWith('/repeaters/319186801/alert', { enabled: true, delay_minutes: 10, back: false });
    expect(w.find('[data-test="alert-email"]').text()).toBe('Emails go to k***@example.org.');

    await w.find('[data-test="alert-delay"]').setValue('30');
    await flushPromises();
    expect(mockAPI.put).toHaveBeenLastCalledWith('/repeaters/319186801/alert', { enabled: true, delay_minutes: 30, back: false });
    await w.find('[data-test="alert-back"]').setValue(true);
    await flushPromises();
    expect(mockAPI.put).toHaveBeenLastCalledWith('/repeaters/319186801/alert', { enabled: true, delay_minutes: 30, back: true });
  });

  it('asks for a verified email before the alert can be turned on', async () => {
    mockAPI.get.mockResolvedValue({ data: state({ email_verified: false, email: '' }) });
    const w = card();
    await flushPromises();
    expect(w.find('[data-test="alert-no-email"]').text()).toContain('verify it to get these emails');
    expect(w.find('[data-test="alert-on"]').attributes('disabled')).toBeDefined();
  });

  it('shows why a save failed', async () => {
    mockAPI.get.mockResolvedValue({ data: state() });
    mockAPI.put.mockRejectedValue({ response: { data: { error: 'Verify an email address in Settings first' } } });
    const w = card();
    await flushPromises();
    await w.find('[data-test="alert-on"]').trigger('click');
    await flushPromises();
    expect(w.find('[data-test="alert-message"]').text()).toBe('Verify an email address in Settings first');
  });
});
