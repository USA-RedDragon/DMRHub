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

export type OnAirCall = {
  call_id: number;
  slot: number;
  direction: 'from' | 'to';
  source_id: number;
  callsign: string;
  destination_id: number;
  group_call: boolean;
  start_time: string;
};

/** callLength returns the time from start to now as m:ss. */
export function callLength(start: string, now: number): string {
  const s = Math.max(0, Math.floor((now - new Date(start).getTime()) / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

/** resetText returns what a reset of a device does while calls are on the air. */
export function resetText(device: string, calls: OnAirCall[]): string {
  const slots = [...new Set(calls.map((c) => `TS${c.slot}`))].sort();
  const ends = slots.length === 0 ? '' : `The ${slots.length > 1 ? 'calls' : 'call'} on ${slots.join(' and ')} ${slots.length > 1 ? 'end' : 'ends'}. `;
  return `${ends}The ${device} logs in again by itself.`;
}
