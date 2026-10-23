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
import TypeFilter from '@/components/TypeFilter.vue';
import TransmissionType from '@/components/TransmissionType.vue';
import { defaultFilter, type TypeFilterValue } from '@/lib/transmissions';

describe('TypeFilter', () => {
  it('shows every type, with unknown off by default', () => {
    const wrapper = mount(TypeFilter, { props: { modelValue: defaultFilter() } });
    const chips = wrapper.findAll('[data-kind]');
    expect(chips.map((c) => c.text())).toEqual(['Voice', 'Data', 'SMS', 'GPS', 'Talker Alias', 'CSBK', 'Control', 'Unknown']);
    expect(chips.map((c) => c.attributes('aria-pressed'))).toEqual(['true', 'true', 'true', 'true', 'true', 'true', 'true', 'false']);
    expect(wrapper.text()).toContain('Show idle and USBD rows');
    expect((wrapper.find('[data-testid="type-idle"]').element as HTMLInputElement).checked).toBe(false);
  });

  it('toggles a type and keeps the order', async () => {
    const wrapper = mount(TypeFilter, { props: { modelValue: defaultFilter() } });
    await wrapper.find('[data-kind="sms"]').trigger('click');
    let emitted = wrapper.emitted('update:modelValue') as TypeFilterValue[][];
    expect(emitted[0]![0]!.types).toEqual(['voice', 'data', 'gps', 'alias', 'csbk', 'control']);

    await wrapper.setProps({ modelValue: { types: ['voice'], idle: false } });
    await wrapper.find('[data-kind="unknown"]').trigger('click');
    emitted = wrapper.emitted('update:modelValue') as TypeFilterValue[][];
    expect(emitted[1]![0]!.types).toEqual(['voice', 'unknown']);
  });

  it('selects every type and idle rows with All', async () => {
    const wrapper = mount(TypeFilter, { props: { modelValue: { types: [], idle: false } } });
    await wrapper.find('[data-testid="type-all"]').trigger('click');
    const emitted = wrapper.emitted('update:modelValue') as TypeFilterValue[][];
    expect(emitted[0]![0]).toEqual({
      types: ['voice', 'data', 'sms', 'gps', 'alias', 'csbk', 'control', 'unknown'],
      idle: true,
    });
  });

  it('sets the idle flag', async () => {
    const wrapper = mount(TypeFilter, { props: { modelValue: defaultFilter() } });
    await wrapper.find('[data-testid="type-idle"]').setValue(true);
    const emitted = wrapper.emitted('update:modelValue') as TypeFilterValue[][];
    expect(emitted[0]![0]!.idle).toBe(true);
  });
});

describe('TransmissionType', () => {
  it('shows the badge and the detail lines', () => {
    const wrapper = mount(TransmissionType, {
      props: {
        call: {
          id: 1, active: false, start_time: new Date(), time_slot: false, group_call: false, repeater_id: 0,
          repeater: { id: 0, callsign: '' }, user: { id: 1, callsign: 'A' }, is_to_talkgroup: false, to_talkgroup: { id: 0 },
          is_to_repeater: false, to_repeater: { id: 0, callsign: '' }, is_to_user: true, to_user: { id: 2, callsign: 'B' },
          destination_id: 2, duration: '0.1', ber: '0', loss: '0', jitter: '0', rssi: '0',
          kind: 'csbk', sub_kind: 'radio_check', fid: 16,
        },
      },
    });
    expect(wrapper.find('[data-testid="type-badge"]').text()).toBe('CSBK');
    expect(wrapper.text()).toContain('Radio Check');
    expect(wrapper.text()).toContain('Motorola FID 0x10');
  });
});
