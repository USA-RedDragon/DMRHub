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

import { hillshadeOpacity, nodataMask, remapHillshade, remapPixel } from '@/lib/map/hillshade';

const W = 32;

function tile(fill: number): Uint8Array {
  return new Uint8Array(W * W).fill(fill);
}

function paint(grey: Uint8Array, x0: number, y0: number, x1: number, y1: number, v: number): void {
  for (let y = y0; y < y1; y++) {
    for (let x = x0; x < x1; x++) {
      grey[y * W + x] = v;
    }
  }
}

function rgba(grey: Uint8Array): Uint8Array {
  const out = new Uint8Array(grey.length * 4);
  grey.forEach((v, i) => out.set([v, v, v, 255], i * 4));
  return out;
}

describe('remapPixel', () => {
  it('draws nothing on flat ground', () => {
    for (const v of [179, 180, 181, 182, 183]) {
      expect(remapPixel(v)).toEqual([0, 0, 0, 0]);
    }
  });

  it('draws shadow black and lit slopes white, with alpha from the relief', () => {
    expect(remapPixel(178)).toEqual([0, 0, 0, 3]);
    expect(remapPixel(184)).toEqual([255, 255, 255, 3]);
    expect(remapPixel(130)).toEqual([0, 0, 0, 147]);
    expect(remapPixel(219)).toEqual([255, 255, 255, 108]);
  });

  it('saturates at full ink', () => {
    expect(remapPixel(0)).toEqual([0, 0, 0, 255]);
    expect(remapPixel(255)).toEqual([255, 255, 255, 216]);
    expect(remapPixel(181 - 87)).toEqual([0, 0, 0, 255]);
  });
});

describe('nodataMask', () => {
  it('takes a dark region bordered by flat land for water', () => {
    const grey = tile(181);
    paint(grey, 10, 10, 14, 14, 2);
    const mask = nodataMask(W, grey);
    expect(mask[12 * W + 12]).toBe(1);
    expect(mask[0]).toBe(0);
  });

  it('takes a dark region bordered by shadow for deep shadow', () => {
    const grey = tile(181);
    paint(grey, 6, 6, 20, 20, 30);
    paint(grey, 10, 10, 14, 14, 2);
    expect(nodataMask(W, grey)[12 * W + 12]).toBe(0);
  });

  it('takes a tile that is all dark for water', () => {
    expect(nodataMask(W, tile(0)).every((v) => v === 1)).toBe(true);
  });

  it('finds water under a shadowed wall by its solid core of zeros', () => {
    const grey = tile(20);
    paint(grey, 0, 16, W, W, 0);
    paint(grey, 0, 14, W, 16, 3);
    const mask = nodataMask(W, grey);
    expect(mask[24 * W + 16]).toBe(1);
    expect(mask[15 * W + 16]).toBe(1);
    expect(mask[4 * W + 16]).toBe(0);
  });

  it('leaves a small solid block of zeros in shadow as shadow', () => {
    const grey = tile(20);
    paint(grey, 10, 10, 15, 15, 0);
    expect(nodataMask(W, grey)[12 * W + 12]).toBe(0);
  });
});

describe('remapHillshade', () => {
  it('draws deep shadow opaque black and water not at all', () => {
    const grey = tile(181);
    paint(grey, 0, 0, 8, 8, 2);
    paint(grey, 20, 20, 30, 30, 40);
    paint(grey, 24, 24, 26, 26, 1);
    const out = remapHillshade(W, rgba(grey));
    expect([...out.subarray((4 * W + 4) * 4, (4 * W + 4) * 4 + 4)]).toEqual([0, 0, 0, 0]);
    expect([...out.subarray((25 * W + 25) * 4, (25 * W + 25) * 4 + 4)]).toEqual([0, 0, 0, 255]);
    expect(out[(16 * W + 16) * 4 + 3]).toBe(0);
  });
});

describe('hillshadeOpacity', () => {
  it('fades out from zoom 13 to 15, lighter on the light theme', () => {
    expect(hillshadeOpacity(true)).toEqual(['interpolate', ['linear'], ['zoom'], 13, 1, 15, 0]);
    expect(hillshadeOpacity(false)).toEqual(['interpolate', ['linear'], ['zoom'], 13, 0.55, 15, 0]);
  });
});
