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
import { netSchedule } from '@/lib/timeConversion';

describe('netSchedule', () => {
  const october = new Date('2026-10-27T12:00:00Z');
  const december = new Date('2026-12-01T12:00:00Z');

  it('shows the UTC slot in the time zone of the net', () => {
    expect(netSchedule(2, '19:00', 'America/Chicago', october)).toBe('Tue at 14:00 America/Chicago');
    expect(netSchedule(3, '00:00', 'America/Chicago', october)).toBe('Tue at 19:00 America/Chicago');
  });

  it('follows the time zone of the net across a DST change', () => {
    expect(netSchedule(3, '00:00', 'America/Chicago', december)).toBe('Tue at 18:00 America/Chicago');
  });

  it('shows UTC for a net with no time zone or an unknown one', () => {
    expect(netSchedule(2, '19:00', '', october)).toBe('Tue at 19:00 UTC');
    expect(netSchedule(2, '19:00', 'Not/AZone', october)).toBe('Tue at 19:00 UTC');
  });
});
