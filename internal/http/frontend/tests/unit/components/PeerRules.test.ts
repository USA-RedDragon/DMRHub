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
  default: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn() },
}));
vi.mock('@/components/ui/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import API from '@/services/API';
import PeerRules from '@/components/peer/PeerRules.vue';

const mockAPI = API as unknown as { get: Mock; post: Mock; patch: Mock; delete: Mock };

const RULES = [
  { id: 11, direction: true, match: 'destination', subject_id_min: 91, subject_id_max: 91 },
  { id: 14, direction: false, match: 'destination', subject_id_min: 3100, subject_id_max: 3100 },
  { id: 15, direction: false, match: 'destination', subject_id_min: 31665, subject_id_max: 31665 },
  { id: 16, direction: false, match: 'source', subject_id_min: 3000001, subject_id_max: 3000099 },
];

async function mountRules(props: Record<string, unknown> = {}, rules: unknown[] = RULES) {
  mockAPI.get.mockResolvedValue({ data: { rules } });
  const w = mount(PeerRules, {
    props: { peerId: 3000, ingress: true, egress: true, ingressPrivate: false, egressPrivate: true, ...props },
  });
  await flushPromises();
  return w;
}

describe('PeerRules', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('lists the rules and sums them up', async () => {
    const w = await mountRules();
    expect(mockAPI.get).toHaveBeenCalledWith('/peers/3000/rules');
    expect(w.find('[data-test="summary-rx"]').text()).toBe('Receives calls to 91.');
    expect(w.find('[data-test="summary-tx"]').text()).toBe(
      'Transmits calls to 3100 and 31665, and private calls, from radios 3000001 to 3000099.',
    );
    expect(w.find('[data-test="rule-16"]').text()).toContain('From radios');
    expect(w.find('[data-test="rule-16"]').text()).toContain('3000001 to 3000099');
    expect((w.find('[data-test="private-tx"]').element as HTMLInputElement).checked).toBe(true);
  });

  it('says when a direction is off', async () => {
    const w = await mountRules({ egress: false });
    expect(w.find('[data-test="summary-tx"]').text()).toBe('Transmit is off.');
  });

  it('shows no rules', async () => {
    const w = await mountRules({ egressPrivate: false }, []);
    expect(w.find('[data-test="summary-tx"]').text()).toBe('Transmits nothing.');
    expect(w.find('[data-test="group-tx"]').text()).toContain('No transmit rules');
  });

  it('adds a source rule', async () => {
    const w = await mountRules();
    mockAPI.post.mockResolvedValue({
      data: { rule: { id: 20, direction: true, match: 'source', subject_id_min: 3100001, subject_id_max: 3199999 } },
    });
    await w.find('[data-test="add-rx"]').trigger('click');
    await w.find('[data-test="form-rx"] [data-test="kind-source"]').trigger('click');
    await w.find('[data-test="form-rx"] [data-test="min"]').setValue('3100001');
    await w.find('[data-test="form-rx"] [data-test="max"]').setValue('3199999');
    await w.find('[data-test="form-rx"]').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/peers/3000/rules', {
      direction: true,
      match: 'source',
      subject_id_min: 3100001,
      subject_id_max: 3199999,
    });
    expect(w.find('[data-test="summary-rx"]').text()).toBe('Receives calls to 91 from radios 3100001 to 3199999.');
    expect(w.find('[data-test="form-rx"]').exists()).toBe(false);
  });

  it('adds a talkgroup with one ID', async () => {
    const w = await mountRules({}, []);
    mockAPI.post.mockResolvedValue({
      data: { rule: { id: 21, direction: false, match: 'destination', subject_id_min: 3100, subject_id_max: 3100 } },
    });
    await w.find('[data-test="add-tx"]').trigger('click');
    await w.find('[data-test="form-tx"] [data-test="min"]').setValue('3100');
    await w.find('[data-test="form-tx"]').trigger('submit');
    await flushPromises();
    expect(mockAPI.post).toHaveBeenCalledWith('/peers/3000/rules', {
      direction: false,
      match: 'destination',
      subject_id_min: 3100,
      subject_id_max: 3100,
    });
  });

  it('refuses a bad range', async () => {
    const w = await mountRules();
    await w.find('[data-test="add-tx"]').trigger('click');
    await w.find('[data-test="form-tx"] [data-test="min"]').setValue('200');
    await w.find('[data-test="form-tx"] [data-test="max"]').setValue('100');
    await w.find('[data-test="form-tx"]').trigger('submit');
    expect(w.find('[data-test="error"]').text()).toBe('The last ID must not be lower than the first ID.');
    await w.find('[data-test="form-tx"] [data-test="min"]').setValue('x');
    await w.find('[data-test="form-tx"]').trigger('submit');
    expect(w.find('[data-test="error"]').text()).toBe('Enter whole numbers.');
    expect(mockAPI.post).not.toHaveBeenCalled();
  });

  it('deletes a rule after a confirm', async () => {
    const w = await mountRules();
    mockAPI.delete.mockResolvedValue({ data: {} });
    await w.find('[data-test="delete-14"]').trigger('click');
    expect(mockAPI.delete).not.toHaveBeenCalled();
    await w.find('[data-test="really-14"]').trigger('click');
    await flushPromises();
    expect(mockAPI.delete).toHaveBeenCalledWith('/peers/3000/rules/14');
    expect(w.find('[data-test="rule-14"]').exists()).toBe(false);
  });

  it('saves the private call switch', async () => {
    const w = await mountRules();
    mockAPI.patch.mockResolvedValue({ data: {} });
    await w.find('[data-test="private-rx"]').setValue(true);
    await flushPromises();
    expect(mockAPI.patch).toHaveBeenCalledWith('/peers/3000', { ingress_private: true });
    expect(w.emitted('private-changed')).toEqual([[{ ingress: true, on: true }]]);
    expect(w.find('[data-test="summary-rx"]').text()).toBe('Receives calls to 91, and private calls.');
  });

  it('puts the switch back when the save fails', async () => {
    const w = await mountRules();
    mockAPI.patch.mockRejectedValue(new Error('down'));
    await w.find('[data-test="private-tx"]').setValue(false);
    await flushPromises();
    expect((w.find('[data-test="private-tx"]').element as HTMLInputElement).checked).toBe(true);
  });

  it('hides the private call switch where it changes nothing', async () => {
    const w = await mountRules({}, [{ id: 1, direction: false, match: 'source', subject_id_min: 1, subject_id_max: 2 }]);
    expect(w.find('[data-test="private-tx"]').exists()).toBe(false);
    expect(w.find('[data-test="private-rx"]').exists()).toBe(true);
  });
});
