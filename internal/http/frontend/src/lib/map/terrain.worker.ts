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

import { PMTiles } from 'pmtiles';

import { archiveSource, PART_BYTES, TERRAIN_URL } from './archive';
import { remapHillshade } from './hillshade';

/** TileAsk asks the terrain worker for one hillshade tile, or to drop an ask it no longer needs. */
export type TileAsk = { id: number; z: number; x: number; y: number } | { cancel: number };

/** TileAnswer carries one remapped tile back, or why there is none. */
export type TileAnswer = { id: number; bitmap?: ImageBitmap; error?: string };

const scope = self as unknown as {
  postMessage: (message: TileAnswer, transfer?: Transferable[]) => void;
  onmessage: ((e: MessageEvent<TileAsk>) => void) | null;
};

let archive: Promise<PMTiles> | null = null;
const cancelled = new Set<number>();

function open(): Promise<PMTiles> {
  if (!archive) {
    archive = archiveSource(TERRAIN_URL, PART_BYTES).then((s) => new PMTiles(s));
    archive.catch(() => {
      archive = null;
    });
  }
  return archive;
}

function blank(): Promise<ImageBitmap> {
  return createImageBitmap(new ImageData(1, 1));
}

async function tile(id: number, z: number, x: number, y: number): Promise<ImageBitmap | null> {
  const res = await (await open()).getZxy(z, x, y);
  if (cancelled.delete(id)) {
    return null;
  }
  if (!res?.data.byteLength) {
    return blank();
  }
  const decoded = await createImageBitmap(new Blob([res.data], { type: 'image/webp' }));
  const { width, height } = decoded;
  const canvas = new OffscreenCanvas(width, height);
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  if (!ctx) {
    decoded.close();
    throw new Error('no 2D canvas in the terrain worker');
  }
  ctx.drawImage(decoded, 0, 0);
  decoded.close();
  const out = remapHillshade(width, ctx.getImageData(0, 0, width, height).data);
  if (!out.some((v, i) => i % 4 === 3 && v !== 0)) {
    return blank();
  }
  return createImageBitmap(new ImageData(out, width, height));
}

scope.onmessage = (e) => {
  if ('cancel' in e.data) {
    cancelled.add(e.data.cancel);
    return;
  }
  const { id, z, x, y } = e.data;
  tile(id, z, x, y).then(
    (bitmap) => {
      cancelled.delete(id);
      if (bitmap) {
        scope.postMessage({ id, bitmap }, [bitmap]);
      }
    },
    (err: unknown) => {
      cancelled.delete(id);
      scope.postMessage({ id, error: err instanceof Error ? err.message : String(err) });
    },
  );
};
