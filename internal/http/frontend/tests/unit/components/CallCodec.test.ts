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
import CallCodec from '@/components/CallCodec.vue';
import { badgeClass } from '@/lib/badge';
import { lastHeardColumns } from '@/lib/lastHeardColumns';
import type { CallRow } from '@/lib/transmissions';

const call = (fields: Partial<CallRow>): CallRow => ({
  id: 1, active: false, start_time: new Date(), time_slot: false, group_call: true, repeater_id: 0,
  repeater: { id: 0, callsign: '' }, user: { id: 1000001, callsign: 'N0CALL' }, is_to_talkgroup: false,
  to_talkgroup: { id: 0 }, is_to_repeater: false, to_repeater: { id: 0, callsign: '' }, is_to_user: false,
  to_user: { id: 0, callsign: '' }, destination_id: 91, duration: '6.2', ber: '0', loss: '0', jitter: '0', rssi: '0',
  kind: 'voice',
  ...fields,
});

const render = (fields: Partial<CallRow>) => mount(CallCodec, { props: { call: call(fields) } });

describe('CallCodec', () => {
  it('shows a Codec2 call sent as AMBE in blue', () => {
    const wrapper = render({ codec: 'codec2', codec2_mode: 3200, sent_ambe: 4 });
    const badge = wrapper.find('[data-testid="codec-badge"]');
    expect(badge.text()).toBe('Codec2 3200');
    expect(badge.classes().join(' ')).toBe(badgeClass('info'));
    expect(wrapper.text()).toContain('Sent as AMBE to 4 targets');
  });

  it('shows an AMBE call sent as Codec2 in zinc', () => {
    const wrapper = render({ codec: 'ambe', sent_codec2: 1 });
    const badge = wrapper.find('[data-testid="codec-badge"]');
    expect(badge.text()).toBe('AMBE');
    expect(badge.classes().join(' ')).toBe(badgeClass('none'));
    expect(wrapper.text()).toContain('Sent as Codec2 to 1 target');
  });

  it('turns yellow for audio that was not converted', () => {
    const wrapper = render({ codec: 'codec2', codec2_mode: 3200, sent_ambe: 4, missed_ms: 1200 });
    const badge = wrapper.find('[data-testid="codec-badge"]');
    expect(badge.classes().join(' ')).toBe(badgeClass('warn'));
    expect(wrapper.text()).toContain('1.2 s not converted: AMBE server did not answer');
    expect(wrapper.text()).not.toContain('Sent as');
  });

  it('turns yellow with only the busy line when every AMBE server was busy', () => {
    const wrapper = render({ codec: 'codec2', codec2_mode: 3200, busy: true, missed_ms: 1200, sent_ambe: 4 });
    const badge = wrapper.find('[data-testid="codec-badge"]');
    expect(badge.text()).toBe('Codec2 3200');
    expect(badge.classes().join(' ')).toBe(badgeClass('warn'));
    const lines = wrapper.findAll('span.text-muted-foreground').map((l) => l.text());
    expect(lines).toEqual(['Not converted: every AMBE server was busy']);
  });

  it('shows the usual lines when busy is false', () => {
    const wrapper = render({ codec: 'codec2', codec2_mode: 3200, busy: false, sent_ambe: 4 });
    expect(wrapper.find('[data-testid="codec-badge"]').classes().join(' ')).toBe(badgeClass('info'));
    expect(wrapper.text()).toContain('Sent as AMBE to 4 targets');
    expect(wrapper.text()).not.toContain('busy');
  });

  it('shows only the badge when nothing was converted', () => {
    const wrapper = render({ codec: 'ambe', sent_ambe: 0, sent_codec2: 0, missed_ms: 0 });
    expect(wrapper.text()).toBe('AMBE');
  });

  it('takes a row without a kind as voice', () => {
    expect(render({ kind: '', codec: 'codec2', codec2_mode: 0 }).text()).toBe('Codec2');
    expect(render({ kind: undefined, codec: 'codec2', codec2_mode: 2400 }).text()).toBe('Codec2 2400');
  });

  it('is empty for a call without a codec and for rows that are not voice', () => {
    expect(render({ codec: '' }).find('[data-testid="codec-badge"]').exists()).toBe(false);
    expect(render({}).find('[data-testid="codec-badge"]').exists()).toBe(false);
    expect(render({ kind: 'data', codec: 'ambe' }).find('[data-testid="codec-badge"]').exists()).toBe(false);
    expect(render({ kind: 'csbk', codec: 'codec2' }).find('[data-testid="codec-badge"]').exists()).toBe(false);
  });
});

describe('lastHeardColumns', () => {
  it('puts the Codec column right after Duration in every table', () => {
    for (const opts of [{}, { repeater: true }, { user: false }, { destination: false }]) {
      const headers = lastHeardColumns(opts).map((c) => c.header);
      const i = headers.indexOf('Duration');
      expect(i).toBeGreaterThan(-1);
      expect(headers[i + 1]).toBe('Codec');
    }
  });
});
