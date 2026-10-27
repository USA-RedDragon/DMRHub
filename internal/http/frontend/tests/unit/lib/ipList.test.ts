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

import { describe, expect, it } from 'vitest';
import { ipEntryError, ipListCount, ipListError } from '@/lib/ipList';

describe('IP list entries', () => {
  it('takes addresses and ranges of IPv4 and IPv6', () => {
    for (const e of [
      '203.0.113.7', ' 198.51.100.0/24 ', '0.0.0.0/0', '2001:db8::1', '2001:db8::/32', '::/0', '::1',
      '::ffff:203.0.113.7', '::ffff:203.0.113.0/120', '2001:0db8:0000:0000:0000:0000:0000:0001', '',
    ]) {
      expect({ e, error: ipEntryError(e) }).toEqual({ e, error: undefined });
    }
  });

  it('refuses what is not an address or a range', () => {
    for (const e of [
      'example.org', '203.0.113', '203.0.113.256', '203.0.113.0/33', '2001:db8::/129', 'fe80::1%eth0',
      '2001:db8::1::2', '1:2:3:4:5:6:7:8:9', '10.0.0.0/8/8', '10.0.0.0/x',
    ]) {
      expect(ipEntryError(e)).toBe(`${e} is not an IP address or CIDR range`);
    }
  });

  it('names the range of an entry with bits set after its prefix', () => {
    expect(ipEntryError('10.1.2.3/8')).toBe('10.1.2.3/8 has bits set after its prefix; use 10.0.0.0/8');
    expect(ipEntryError('2001:db8::1/32')).toBe('2001:db8::1/32 has bits set after its prefix; use 2001:db8::/32');
    expect(ipEntryError('2001:db8:0:1::/48')).toBe('2001:db8:0:1::/48 has bits set after its prefix; use 2001:db8::/48');
  });

  it('gives the error of the first bad entry and counts the entries', () => {
    expect(ipListError(['203.0.113.7', '', 'nope', '10.1.2.3/8'])).toBe('nope is not an IP address or CIDR range');
    expect(ipListError(['203.0.113.7', '2001:db8::/32'])).toBeUndefined();
    expect(ipListError(undefined)).toBeUndefined();
    expect(ipListCount(['203.0.113.7', ' ', '2001:db8::/32'])).toBe(2);
    expect(ipListCount(undefined)).toBe(0);
  });
});
