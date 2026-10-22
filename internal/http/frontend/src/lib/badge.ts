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

export type BadgeTone = 'info' | 'ok' | 'warn' | 'bad' | 'none';

const base = 'inline-flex items-center whitespace-nowrap rounded-md px-2.5 py-0.5 text-xs font-medium';

const tones: Record<BadgeTone, string> = {
  info: 'bg-blue-600/10 text-blue-600 dark:bg-blue-500/10 dark:text-blue-500',
  ok: 'bg-green-600/10 text-green-600 dark:bg-green-500/10 dark:text-green-500',
  warn: 'bg-yellow-600/10 text-yellow-600 dark:bg-yellow-500/10 dark:text-yellow-500',
  bad: 'bg-red-600/10 text-red-600 dark:bg-red-500/10 dark:text-red-500',
  none: 'bg-zinc-500/10 text-zinc-500 dark:bg-zinc-400/10 dark:text-zinc-400',
};

/** badgeClass returns the classes of a small tinted status badge. */
export function badgeClass(tone: BadgeTone): string {
  return `${base} ${tones[tone]}`;
}
