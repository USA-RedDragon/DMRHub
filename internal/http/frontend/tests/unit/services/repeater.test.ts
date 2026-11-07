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
import { repeaterOnline } from '@/services/repeater';

const now = Date.parse('2026-11-06T12:00:00Z');
const ago = (ms: number) => new Date(now - ms).toISOString();

describe('repeaterOnline', () => {
  it('follows the last ping and the end of the session', () => {
    expect(repeaterOnline({ last_ping_time: ago(5_000) }, now)).toBe(true);
    expect(repeaterOnline({ last_ping_time: ago(5_000), disconnected_time: null }, now)).toBe(true);
    expect(repeaterOnline({ last_ping_time: '0001-01-01T00:00:00Z' }, now)).toBe(false);
    expect(repeaterOnline({ last_ping_time: ago(60_000) }, now)).toBe(false);
    expect(repeaterOnline({ last_ping_time: ago(5_000), disconnected_time: ago(1_000) }, now)).toBe(false);
    expect(repeaterOnline({ last_ping_time: ago(5_000), disconnected_time: ago(5_000) }, now)).toBe(false);
    expect(repeaterOnline({ last_ping_time: ago(5_000), disconnected_time: ago(30_000) }, now)).toBe(true);
  });
});
