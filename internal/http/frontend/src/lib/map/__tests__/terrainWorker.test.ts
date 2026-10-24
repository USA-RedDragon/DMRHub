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

import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('../archive', () => ({
  archiveSource: vi.fn(async () => ({})),
  PART_BYTES: 1,
  TERRAIN_URL: 'https://tiles.example/terrain.pmtiles',
}));

vi.mock('pmtiles', () => ({
  PMTiles: class {
    getZxy() {
      return Promise.resolve({ data: new ArrayBuffer(8) });
    }
  },
}));

const SIZE = 4;

class FakeImageData {
  data: Uint8ClampedArray;
  width: number;
  height: number;
  constructor(data: Uint8ClampedArray | number, width?: number, height?: number) {
    if (typeof data === 'number') {
      this.width = data;
      this.height = width ?? data;
      this.data = new Uint8ClampedArray(this.width * this.height * 4);
    } else {
      this.data = data;
      this.width = width ?? 0;
      this.height = height ?? 0;
    }
  }
}

class FakeCanvas {
  getContext() {
    return {
      drawImage: () => undefined,
      getImageData: () => {
        const grey = new Uint8ClampedArray(SIZE * SIZE * 4);
        for (let i = 0; i < grey.length; i += 4) {
          grey[i] = grey[i + 1] = grey[i + 2] = 230;
          grey[i + 3] = 255;
        }
        return { data: grey };
      },
    };
  }
}

describe('terrain worker', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('hands MapLibre premultiplied bitmaps, which its raster shader expects', async () => {
    const created: { source: unknown; options?: ImageBitmapOptions }[] = [];
    vi.stubGlobal('ImageData', FakeImageData);
    vi.stubGlobal('OffscreenCanvas', FakeCanvas);
    vi.stubGlobal('createImageBitmap', vi.fn(async (source: unknown, options?: ImageBitmapOptions) => {
      created.push({ source, options });
      return { width: SIZE, height: SIZE, close: () => undefined };
    }));
    const posted: unknown[] = [];
    vi.stubGlobal('postMessage', (m: unknown) => posted.push(m));
    await import('../terrain.worker');
    const scope = self as unknown as { onmessage: (e: { data: unknown }) => void };
    scope.onmessage({ data: { id: 1, z: 5, x: 6, y: 12 } });
    await vi.waitFor(() => expect(posted).toHaveLength(1));
    const out = created.filter((c) => c.source instanceof FakeImageData);
    expect(out).toHaveLength(1);
    expect(out[0]!.options?.premultiplyAlpha).toBe('premultiply');
  });
});
