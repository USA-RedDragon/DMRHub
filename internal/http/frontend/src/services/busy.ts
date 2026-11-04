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

let pieces = 0;
let routing = false;

/** show writes the count to the html element, when there is a document: a timer of end can fire after the document went away, such as after a test. */
export function show(): void {
  if (typeof document === 'undefined' || !document) return;
  document.documentElement.dataset.busy = String(pieces + (routing ? 1 : 0));
}

show();

/**
 * begin counts a piece of work of the page that has started, such as a
 * request. The html element shows the count in
 * data-busy, so that a script that drives the page, such as the docs
 * screenshot harness, can wait until the page has nothing left to load.
 */
export function begin(): void {
  pieces++;
  show();
}

/**
 * end counts a piece of work as done. The count drops only in a later
 * task, after the code that waits on the work ran and Vue drew its result.
 */
export function end(): void {
  setTimeout(() => {
    pieces = Math.max(0, pieces - 1);
    show();
  }, 0);
}

/**
 * routeStarted counts a route change as one piece of work until
 * routeEnded. A guard that redirects starts a second change before the
 * first ends, so the changes are not counted one by one.
 */
export function routeStarted(): void {
  routing = true;
  show();
}

/** routeEnded ends the route change in a later task, after the new page mounted. */
export function routeEnded(): void {
  setTimeout(() => {
    routing = false;
    show();
  }, 0);
}
