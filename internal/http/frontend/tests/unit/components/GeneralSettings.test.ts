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
import GeneralSettings from '@/components/setup/GeneralSettings.vue';

type Emitted = Record<string, unknown>[][];

describe('GeneralSettings auto-approve', () => {
  it('is off by default and says new users wait for an admin', () => {
    const wrapper = mount(GeneralSettings, { props: { modelValue: {}, errors: {} } });
    expect((wrapper.find('#auto-approve-users').element as HTMLInputElement).checked).toBe(false);
    expect(wrapper.find('[data-test="auto-approve-off"]').text()).toContain('waits until an admin approves them');
  });

  it('turns on and keeps the other settings', async () => {
    const wrapper = mount(GeneralSettings, { props: { modelValue: { 'network-name': 'DMRHub' }, errors: {} } });
    await wrapper.find('#auto-approve-users').setValue(true);
    const emitted = wrapper.emitted('update:modelValue') as Emitted;
    expect(emitted[0]![0]).toEqual({ 'network-name': 'DMRHub', 'auto-approve-users': true });
  });

  it('says a new user can log in at once when it is on', () => {
    const wrapper = mount(GeneralSettings, { props: { modelValue: { 'auto-approve-users': true }, errors: {} } });
    expect(wrapper.find('[data-test="auto-approve-on"]').text()).toContain('can log in as soon as they register');
  });
});
