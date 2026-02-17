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
import { localToUTC, utcToLocal } from '@/lib/timeConversion';

describe('timeConversion', () => {
  describe('localToUTC / utcToLocal round-trip', () => {
    // These tests verify that converting local→UTC→local yields the original
    // values. The actual UTC values depend on the system's timezone, so we
    // only check the round-trip invariant.
    const days = [0, 1, 2, 3, 4, 5, 6];
    const times = ['00:00', '06:30', '12:00', '18:45', '23:59'];

    for (const day of days) {
      for (const time of times) {
        it(`round-trips day=${day} time=${time}`, () => {
          const utc = localToUTC(day, time);
          const local = utcToLocal(utc.dayOfWeek, utc.timeOfDay);

          expect(local.dayOfWeek).toBe(day);
          expect(local.timeOfDay).toBe(time);
        });
      }
    }
  });

  describe('localToUTC output format', () => {
    it('returns dayOfWeek as 0-6', () => {
      const result = localToUTC(3, '12:00');
      expect(result.dayOfWeek).toBeGreaterThanOrEqual(0);
      expect(result.dayOfWeek).toBeLessThanOrEqual(6);
    });

    it('returns timeOfDay in HH:MM format', () => {
      const result = localToUTC(1, '08:30');
      expect(result.timeOfDay).toMatch(/^\d{2}:\d{2}$/);
    });
  });

  describe('utcToLocal output format', () => {
    it('returns dayOfWeek as 0-6', () => {
      const result = utcToLocal(5, '21:00');
      expect(result.dayOfWeek).toBeGreaterThanOrEqual(0);
      expect(result.dayOfWeek).toBeLessThanOrEqual(6);
    });

    it('returns timeOfDay in HH:MM format', () => {
      const result = utcToLocal(0, '00:00');
      expect(result.timeOfDay).toMatch(/^\d{2}:\d{2}$/);
    });
  });
});
