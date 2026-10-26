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

/**
 * Convert a local day-of-week + HH:MM to UTC day-of-week + HH:MM.
 * Returns { dayOfWeek: 0-6, timeOfDay: "HH:MM" }.
 */
export function localToUTC(
  localDay: number,
  localTime: string,
): { dayOfWeek: number; timeOfDay: string } {
  const now = new Date();
  const currentDay = now.getDay();
  let diff = localDay - currentDay;
  if (diff < 0) diff += 7;
  const target = new Date(now);
  target.setDate(now.getDate() + diff);
  const parts = localTime.split(':').map(Number);
  const hh = parts[0] ?? 0;
  const mm = parts[1] ?? 0;
  target.setHours(hh, mm, 0, 0);

  const utcDay = target.getUTCDay();
  const utcHH = String(target.getUTCHours()).padStart(2, '0');
  const utcMM = String(target.getUTCMinutes()).padStart(2, '0');
  return { dayOfWeek: utcDay, timeOfDay: `${utcHH}:${utcMM}` };
}

/**
 * Convert UTC day-of-week + HH:MM back to local day-of-week + HH:MM, in the
 * same week that localToUTC uses: the 7 × 24 hours from local midnight today.
 * Both then agree across a DST change and when the UTC day is not the local
 * day. In a week with a DST change the local times and the UTC times cannot
 * all pair up: a local time that the change skips does not exist, and when
 * the clocks go back, the last local hour of the week falls outside it.
 */
export function utcToLocal(
  utcDay: number,
  utcTime: string,
): { dayOfWeek: number; timeOfDay: string } {
  const start = new Date();
  start.setHours(0, 0, 0, 0);
  const parts = utcTime.split(':').map(Number);
  const hh = parts[0] ?? 0;
  const mm = parts[1] ?? 0;
  const day = 24 * 60 * 60 * 1000;
  let at = Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate(), hh, mm);
  let diff = utcDay - new Date(at).getUTCDay();
  if (diff < 0) diff += 7;
  at += diff * day;
  while (at >= start.getTime() + 7 * day) at -= 7 * day;
  while (at < start.getTime()) at += 7 * day;
  const target = new Date(at);

  const localDay = target.getDay();
  const localHH = String(target.getHours()).padStart(2, '0');
  const localMM = String(target.getMinutes()).padStart(2, '0');
  return { dayOfWeek: localDay, timeOfDay: `${localHH}:${localMM}` };
}
