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

import type { AddProtocolAction } from 'maplibre-gl';

import type { TileAnswer } from './terrain.worker';

/** TERRAIN_SOURCE is the id of the hillshade source in the map's style. */
export const TERRAIN_SOURCE = 'terrain';

/** TERRAIN_PROTOCOL is the URL scheme the hillshade tiles load through. */
export const TERRAIN_PROTOCOL = 'hillshade';

let worker: Worker | null = null;
let next = 0;
const waiting = new Map<number, { resolve: (b: ImageBitmap) => void; reject: (e: Error) => void }>();

function start(): Worker {
  if (!worker) {
    worker = new Worker(new URL('./terrain.worker.ts', import.meta.url), { type: 'module' });
    worker.onmessage = (e: MessageEvent<TileAnswer>) => {
      const w = waiting.get(e.data.id);
      waiting.delete(e.data.id);
      if (!w) {
        e.data.bitmap?.close();
      } else if (e.data.bitmap) {
        w.resolve(e.data.bitmap);
      } else {
        w.reject(new Error(e.data.error ?? 'no terrain tile'));
      }
    };
  }
  return worker;
}

/** terrainTile loads one remapped hillshade tile from hillshade://z/x/y, decoded and remapped off the main thread. */
export const terrainTile: AddProtocolAction = (params, abort) => {
  const m = /^hillshade:\/\/(\d+)\/(\d+)\/(\d+)$/.exec(params.url);
  if (!m) {
    return Promise.reject(new Error(`not a terrain tile: ${params.url}`));
  }
  const id = ++next;
  const [z, x, y] = [Number(m[1]), Number(m[2]), Number(m[3])];
  return new Promise((resolve, reject) => {
    waiting.set(id, { resolve: (bitmap) => resolve({ data: bitmap }), reject });
    abort.signal.addEventListener('abort', () => {
      if (waiting.delete(id)) {
        worker?.postMessage({ cancel: id });
        reject(new DOMException('aborted', 'AbortError'));
      }
    });
    start().postMessage({ id, z, x, y });
  });
};
