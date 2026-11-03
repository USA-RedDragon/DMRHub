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

/** Shortest time between two reloads that staleBuildReloader makes. */
export const RELOAD_GAP_MS = 10_000;

const KEY = 'stale-build-reload';

/**
 * staleBuildReloader returns a handler for the vite:preloadError event. A
 * chunk of the build that the page runs fails to load after the server
 * moved to a newer build, so the handler reloads the page into the newer
 * build. It reloads at most once in RELOAD_GAP_MS and otherwise lets the
 * error through.
 */
export function staleBuildReloader(storage: Pick<Storage, 'getItem' | 'setItem'>, now: () => number, reload: () => void): (event: Event) => void {
  return (event: Event) => {
    const last = Number(storage.getItem(KEY) ?? 0);
    if (now() - last < RELOAD_GAP_MS) {
      return;
    }
    event.preventDefault();
    storage.setItem(KEY, String(now()));
    reload();
  };
}
