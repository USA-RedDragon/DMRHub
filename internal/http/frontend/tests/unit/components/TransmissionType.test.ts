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

import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import TransmissionType from '@/components/TransmissionType.vue';
import type { CallRow, UndeliverableEntry } from '@/lib/transmissions';

const call = (fields: Partial<CallRow>): CallRow => ({
  id: 1, active: false, start_time: new Date(), time_slot: false, group_call: true, repeater_id: 0,
  repeater: { id: 0, callsign: '' }, user: { id: 3191868, callsign: 'KI5VMF' }, is_to_talkgroup: true,
  to_talkgroup: { id: 91 }, is_to_repeater: false, to_repeater: { id: 0, callsign: '' }, is_to_user: false,
  to_user: { id: 0, callsign: '' }, destination_id: 91, duration: '6.2', ber: '0', loss: '0', jitter: '0', rssi: '0',
  kind: 'voice',
  ...fields,
});

const entry = (id: number, reason: string, callsign = ''): UndeliverableEntry => ({
  sentence: `IPSC repeater ${id}.`, target: 'ipsc', repeater_id: id, callsign, reason,
});

const render = (fields: Partial<CallRow>) => mount(TransmissionType, { props: { call: call(fields) } });

describe('TransmissionType', () => {
  it('shows the caller a count by reason with the repeaters folded under it', () => {
    const wrapper = render({
      undeliverable: [entry(314926, 'slot_busy', 'W1XPR'), entry(311304200, 'slot_busy', 'K5ABC'), entry(311305510, 'no_ipsc_form')],
      undeliverable_scope: 'all',
    });
    expect(wrapper.findAll('[data-testid="undeliverable"]').map((w) => w.text())).toEqual([
      'Not sent to 3 repeaters',
      '2 busy on that slot · 1 could not carry this burst',
    ]);
    const details = wrapper.find('[data-testid="undeliverable-details"]');
    expect(details.element.tagName).toBe('DETAILS');
    expect((details.element as HTMLDetailsElement).open).toBe(false);
    expect(details.find('summary').text()).toBe('Show the 3 repeaters');
    expect(wrapper.findAll('[data-testid="undeliverable-detail"]').map((w) => w.text())).toEqual([
      'Busy on that slot: W1XPR (314926), K5ABC (311304200)',
      'Could not carry this burst: 311305510',
    ]);
  });

  it('shows the owner of a repeater one line and no list', () => {
    const wrapper = render({ undeliverable: [entry(314926, 'slot_busy', 'W1XPR')], undeliverable_scope: 'own' });
    expect(wrapper.findAll('[data-testid="undeliverable"]').map((w) => w.text())).toEqual([
      'Not sent to your repeater W1XPR (314926): busy on that slot',
    ]);
    expect(wrapper.find('[data-testid="undeliverable-details"]').exists()).toBe(false);
  });

  it('shows anyone else only the type', () => {
    const wrapper = render({ undeliverable: null, undeliverable_scope: '' });
    expect(wrapper.text()).toBe('Voice');
    expect(wrapper.find('details').exists()).toBe(false);
  });
});
