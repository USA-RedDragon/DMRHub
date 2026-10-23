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

/** HILLSHADE_FLAT is the grey level gdaldem writes for flat ground. */
export const HILLSHADE_FLAT = 181;

/** HILLSHADE_FLAT_TOLERANCE is how far from flat a level may sit and still draw nothing, to absorb the lossy encoder's wobble. */
export const HILLSHADE_FLAT_TOLERANCE = 2;

/** HILLSHADE_ALPHA_GAIN is the alpha each level of relief past the tolerance adds, clamped at 255. */
export const HILLSHADE_ALPHA_GAIN = 3;

/** HILLSHADE_NODATA_CEILING is the highest level that may be nodata; what surrounds a region of them decides whether it is. */
export const HILLSHADE_NODATA_CEILING = 5;

/** HILLSHADE_NODATA_SHORE_FLOOR is how bright at least half of a dark region's border must be for the region to be water. */
export const HILLSHADE_NODATA_SHORE_FLOOR = 100;

/** HILLSHADE_NODATA_CORE_SIDE is the side of a square of zeros that marks the core of a body of water. */
export const HILLSHADE_NODATA_CORE_SIDE = 5;

/** HILLSHADE_NODATA_CORE_MIN_PX is the fewest pixels a run of core pixels needs to count. */
export const HILLSHADE_NODATA_CORE_MIN_PX = 64;

/** HILLSHADE_NODATA_CORE_REACH is how many steps through dark pixels nodata reaches out from a counted core. */
export const HILLSHADE_NODATA_CORE_REACH = 24;

/** HILLSHADE_FADE_ZOOMS is the zoom the shading starts to fade at and the one it is gone by, since its elevation data counts buildings as hills. */
export const HILLSHADE_FADE_ZOOMS: readonly [number, number] = [13, 15];

/** HILLSHADE_LIGHT_STRENGTH is how strongly the shading draws on the light theme, as a share of the dark theme's. */
export const HILLSHADE_LIGHT_STRENGTH = 0.55;

/** remapPixel turns one grey level into unmultiplied RGBA: black for shadow, white for lit slopes, alpha from the relief. */
export function remapPixel(v: number): [number, number, number, number] {
  const relief = v - HILLSHADE_FLAT;
  const past = Math.abs(relief) - HILLSHADE_FLAT_TOLERANCE;
  if (past <= 0) {
    return [0, 0, 0, 0];
  }
  const alpha = Math.min(255, past * HILLSHADE_ALPHA_GAIN);
  return relief < 0 ? [0, 0, 0, alpha] : [255, 255, 255, alpha];
}

/** remapHillshade turns a decoded tile, RGBA as a canvas hands it over, into the overlay to draw, reading the grey from the red channel. */
export function remapHillshade(width: number, rgba: Uint8ClampedArray | Uint8Array): Uint8ClampedArray<ArrayBuffer> {
  const grey = new Uint8Array(rgba.length / 4);
  for (let i = 0; i < grey.length; i++) {
    grey[i] = rgba[i * 4]!;
  }
  const nodata = nodataMask(width, grey);
  const out = new Uint8ClampedArray(rgba.length);
  for (let i = 0; i < grey.length; i++) {
    if (!nodata[i]) {
      out.set(remapPixel(grey[i]!), i * 4);
    }
  }
  return out;
}

/** nodataMask marks the pixels of a grey tile that are water rather than deep shadow. */
export function nodataMask(width: number, grey: Uint8Array): Uint8Array {
  const nodata = shoreNodata(width, grey);
  const water = waterCoreReach(width, grey);
  for (let i = 0; i < nodata.length; i++) {
    nodata[i]! |= water[i]!;
  }
  return nodata;
}

function eachNeighbour(at: number, width: number, height: number, visit: (n: number) => void): void {
  const x = at % width;
  const y = (at - x) / width;
  for (let ny = Math.max(0, y - 1); ny <= Math.min(height - 1, y + 1); ny++) {
    for (let nx = Math.max(0, x - 1); nx <= Math.min(width - 1, x + 1); nx++) {
      const n = ny * width + nx;
      if (n !== at) {
        visit(n);
      }
    }
  }
}

function shoreNodata(width: number, grey: Uint8Array): Uint8Array {
  const nodata = new Uint8Array(grey.length);
  if (width === 0 || !grey.some((v) => v <= HILLSHADE_NODATA_CEILING)) {
    return nodata;
  }
  const height = grey.length / width;
  const seen = new Uint8Array(grey.length);
  const shoreOf = new Uint32Array(grey.length);
  const region: number[] = [];
  const stack: number[] = [];
  let id = 0;
  for (let start = 0; start < grey.length; start++) {
    if (seen[start] || grey[start]! > HILLSHADE_NODATA_CEILING) {
      continue;
    }
    id++;
    region.length = 0;
    let shore = 0;
    let bright = 0;
    seen[start] = 1;
    stack.push(start);
    while (stack.length) {
      const at = stack.pop()!;
      region.push(at);
      eachNeighbour(at, width, height, (n) => {
        if (grey[n]! <= HILLSHADE_NODATA_CEILING) {
          if (!seen[n]) {
            seen[n] = 1;
            stack.push(n);
          }
        } else if (shoreOf[n] !== id) {
          shoreOf[n] = id;
          shore++;
          if (grey[n]! >= HILLSHADE_NODATA_SHORE_FLOOR) {
            bright++;
          }
        }
      });
    }
    if (shore === 0 || 2 * bright >= shore) {
      for (const at of region) {
        nodata[at] = 1;
      }
    }
  }
  return nodata;
}

function zeroSquares(width: number, height: number, grey: Uint8Array): Uint8Array {
  const half = Math.floor(HILLSHADE_NODATA_CORE_SIDE / 2);
  const prefix = new Uint16Array(Math.max(width, height) + 1);
  const across = new Uint8Array(grey.length);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      prefix[x + 1] = prefix[x]! + (grey[y * width + x] === 0 ? 1 : 0);
    }
    for (let x = 0; x < width; x++) {
      const from = Math.max(0, x - half);
      const to = Math.min(width, x + half + 1);
      across[y * width + x] = prefix[to]! - prefix[from]! === to - from ? 1 : 0;
    }
  }
  const core = new Uint8Array(grey.length);
  for (let x = 0; x < width; x++) {
    for (let y = 0; y < height; y++) {
      prefix[y + 1] = prefix[y]! + across[y * width + x]!;
    }
    for (let y = 0; y < height; y++) {
      const from = Math.max(0, y - half);
      const to = Math.min(height, y + half + 1);
      core[y * width + x] = prefix[to]! - prefix[from]! === to - from ? 1 : 0;
    }
  }
  return core;
}

function waterCoreReach(width: number, grey: Uint8Array): Uint8Array {
  const reach = new Uint8Array(grey.length);
  if (width === 0 || !grey.includes(0)) {
    return reach;
  }
  const height = grey.length / width;
  const core = zeroSquares(width, height, grey);
  const seen = new Uint8Array(grey.length);
  let frontier: number[] = [];
  const run: number[] = [];
  const rim: number[] = [];
  for (let start = 0; start < grey.length; start++) {
    if (seen[start] || !core[start]) {
      continue;
    }
    run.length = 0;
    rim.length = 0;
    seen[start] = 1;
    run.push(start);
    for (let next = 0; next < run.length; next++) {
      const at = run[next]!;
      let onRim = false;
      eachNeighbour(at, width, height, (n) => {
        if (!core[n]) {
          onRim = true;
        } else if (!seen[n]) {
          seen[n] = 1;
          run.push(n);
        }
      });
      if (onRim) {
        rim.push(at);
      }
    }
    if (run.length >= HILLSHADE_NODATA_CORE_MIN_PX) {
      for (const at of run) {
        reach[at] = 1;
      }
      frontier.push(...rim);
    }
  }
  for (let step = 0; step < HILLSHADE_NODATA_CORE_REACH && frontier.length; step++) {
    const grown: number[] = [];
    for (const at of frontier) {
      eachNeighbour(at, width, height, (n) => {
        if (!reach[n] && grey[n]! <= HILLSHADE_NODATA_CEILING) {
          reach[n] = 1;
          grown.push(n);
        }
      });
    }
    frontier = grown;
  }
  return reach;
}

/** hillshadeOpacity is the layer's opacity by zoom: full through the first fade zoom, gone by the second, and scaled down on the light theme. */
export function hillshadeOpacity(dark: boolean): unknown[] {
  const strength = dark ? 1 : HILLSHADE_LIGHT_STRENGTH;
  return ['interpolate', ['linear'], ['zoom'], HILLSHADE_FADE_ZOOMS[0], strength, HILLSHADE_FADE_ZOOMS[1], 0];
}
