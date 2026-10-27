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
import { clientTabs, connectKind, essid, plain, snippet, snippetText, takeSecret, keepSecret } from '@/lib/enroll';

describe('enroll', () => {
  it('knows the kind of a repeater from its type and ID', () => {
    expect(connectKind('mmdvm', 300000101)).toBe('hotspot');
    expect(connectKind('mmdvm', 3000001)).toBe('hotspot');
    expect(connectKind('', 311860)).toBe('repeater');
    expect(connectKind('ipsc', 311860)).toBe('ipsc');
    expect(connectKind('modem', 311860)).toBeNull();
  });

  it('lists only the clients of the kind', () => {
    expect(clientTabs('hotspot')).toEqual(['pistar', 'wpsd', 'mmdvmhost', 'dmrgateway', 'droidstar', 'hblink3', 'other']);
    expect(clientTabs('repeater')).toEqual(clientTabs('hotspot'));
    expect(clientTabs('ipsc')).toEqual(['cps', 'ipsc-other']);
  });

  it('takes the ESSID from the last two digits of a 9-digit ID', () => {
    expect(essid(300000101)).toBe('01');
    expect(essid(300000199)).toBe('99');
    expect(essid(3000001)).toBe('None');
  });

  it('gives a kept secret once', () => {
    keepSecret(1, 'x');
    expect(takeSecret(1)).toBe('x');
    expect(takeSecret(1)).toBeUndefined();
  });

  it('marks the filled-in values of a snippet', () => {
    const s = snippet`[DMR Network ${plain(2)}]\nAddress=${'host'}`;
    expect(s).toEqual([{ text: '[DMR Network ' }, { text: '2' }, { text: ']\nAddress=' }, { text: 'host', hl: true }]);
    expect(snippetText(s)).toBe('[DMR Network 2]\nAddress=host');
  });
});
