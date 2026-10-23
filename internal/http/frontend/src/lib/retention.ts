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

export type RetentionChoice = { value: number; label: string };

export const RETENTION_CHOICES: RetentionChoice[] = [
  { value: 0, label: 'Keep forever' },
  { value: 30, label: '30 days' },
  { value: 90, label: '90 days' },
  { value: 365, label: '1 year' },
  { value: 730, label: '2 years' },
  { value: 1825, label: '5 years' },
];

/** retentionDays reads a retention setting as whole days; anything else is 0, which keeps records forever. */
export function retentionDays(value: unknown): number {
  const v = Number(value ?? 0);
  return Number.isFinite(v) && v >= 0 ? Math.floor(v) : 0;
}

/** retentionChoices lists the choices, plus the current value as days when it is not one of them. */
export function retentionChoices(current: number): RetentionChoice[] {
  if (RETENTION_CHOICES.some((c) => c.value === current)) {
    return RETENTION_CHOICES;
  }
  const extra = { value: current, label: current === 1 ? '1 day' : `${current} days` };
  return [...RETENTION_CHOICES, extra].sort((a, b) => a.value - b.value);
}
