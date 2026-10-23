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
import WebTransportSettings from '@/components/setup/DMR/WebTransportSettings.vue';
import MMDVMSettings from '@/components/setup/DMR/MMDVMSettings.vue';

type Emitted = Record<string, unknown>[][];

describe('WebTransportSettings', () => {
  it('shows only the switch while WebTransport is off', () => {
    const wrapper = mount(WebTransportSettings, { props: { modelValue: {} } });
    expect(wrapper.text()).toContain('Accept browser hotspots and radios');
    expect(wrapper.find('[data-testid="webtransport-fields"]').exists()).toBe(false);
  });

  it('turns on with the default port', async () => {
    const wrapper = mount(WebTransportSettings, { props: { modelValue: {} } });
    await wrapper.find('#webtransport-enabled').trigger('click');
    const emitted = wrapper.emitted('update:modelValue') as Emitted;
    expect(emitted[0]![0]).toEqual({ enabled: true, port: 62032 });
  });

  it('says DMRHub uses its own certificates until a file is set', async () => {
    const wrapper = mount(WebTransportSettings, { props: { modelValue: { enabled: true, port: 62032 } } });
    expect(wrapper.find('[data-testid="webtransport-own"]').exists()).toBe(true);
    await wrapper.setProps({ modelValue: { 'enabled': true, 'port': 62032, 'cert-file': '/tls/tls.crt' } });
    expect(wrapper.find('[data-testid="webtransport-own"]').exists()).toBe(false);
  });

  it('shows the errors of its fields', () => {
    const wrapper = mount(WebTransportSettings, {
      props: {
        modelValue: { enabled: true, port: 62031 },
        errors: { 'port': 'the WebTransport port must be a port other than the MMDVM port', 'key-file': 'set both' },
      },
    });
    expect(wrapper.text()).toContain('the WebTransport port must be a port other than the MMDVM port');
    expect(wrapper.text()).toContain('set both');
  });

  it('sits in the MMDVM settings and saves under webtransport', async () => {
    const wrapper = mount(MMDVMSettings, { props: { modelValue: { bind: '[::]', port: 62031 } } });
    await wrapper.find('#webtransport-enabled').trigger('click');
    const emitted = wrapper.emitted('update:modelValue') as Emitted;
    expect(emitted[0]![0]).toEqual({ bind: '[::]', port: 62031, webtransport: { enabled: true, port: 62032 } });
  });
});
