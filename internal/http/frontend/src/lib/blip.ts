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

import '@/assets/blip.css';

import type { TxMode } from '@/lib/txActivity';

/** BLIP_RINGS is how many rings a looping blip keeps in flight, one every 400 ms, each for 900 ms. */
export const BLIP_RINGS = 3;

/**
 * blipElement builds a marker element that holds the rings of a blip
 * around a pin of size pixels in color.
 */
export function blipElement(color: string, mode: TxMode, size: number): HTMLElement {
  const outer = document.createElement('div');
  outer.style.pointerEvents = 'none';
  const el = document.createElement('div');
  el.style.setProperty('--blip-color', color);
  el.style.position = 'relative';
  el.style.width = `${size}px`;
  el.style.height = `${size}px`;
  for (let i = 0; i < BLIP_RINGS; i++) {
    const ring = document.createElement('span');
    ring.className = 'tx-blip__ring';
    el.appendChild(ring);
  }
  outer.appendChild(el);
  setBlipMode(outer, mode);
  return outer;
}

/** setBlipMode switches a marker element of blipElement between a loop and a single burst. */
export function setBlipMode(outer: HTMLElement, mode: TxMode) {
  const el = outer.firstElementChild;
  if (el) {
    el.className = `tx-blip tx-blip--${mode}`;
  }
}
