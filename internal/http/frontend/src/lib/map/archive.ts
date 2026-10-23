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

import { FetchSource, type RangeResponse, type Source } from 'pmtiles';

import { BlockCachedSource, generationOf } from './blockCache';

/** TILES_BASE is where the basemap archives and their status live. */
export const TILES_BASE = 'https://tiles.squallar.app';

/** PART_BYTES is the size of every part of an archive but the last. */
export const PART_BYTES = 500_000_000;

const MAGIC = 'PMTiles';

/** Span is the bytes of one part that a range covers, end inclusive. */
export type Span = { part: number; start: number; end: number };

/** partName is the suffix of part n of an archive. */
export function partName(n: number): string {
  return `.part${String(n).padStart(3, '0')}`;
}

/** spans splits the range of length bytes at offset into the parts it covers. */
export function spans(offset: number, length: number, partBytes: number): Span[] {
  const out: Span[] = [];
  let at = offset;
  const stop = offset + length;
  while (at < stop) {
    const part = Math.floor(at / partBytes);
    const start = at - part * partBytes;
    const end = Math.min(partBytes, start + (stop - at)) - 1;
    out.push({ part, start, end });
    at += end - start + 1;
  }
  return out;
}

/** PartsSource reads an archive that is published as parts of partBytes each. */
export class PartsSource implements Source {
  constructor(
    readonly url: string,
    readonly partBytes: number = PART_BYTES,
  ) {}

  getKey(): string {
    return this.url;
  }

  async getBytes(offset: number, length: number, signal?: AbortSignal): Promise<RangeResponse> {
    const pieces = await Promise.all(
      spans(offset, length, this.partBytes).map(async (s) => {
        const res = await fetch(this.url + partName(s.part), {
          headers: { Range: `bytes=${s.start}-${s.end}` },
          signal,
          credentials: 'omit',
        });
        if (res.status === 416) {
          return new ArrayBuffer(0);
        }
        if (!res.ok) {
          throw new Error(`Basemap part ${s.part} answered ${res.status}`);
        }
        const buf = await res.arrayBuffer();
        return res.status === 200 ? buf.slice(s.start, s.end + 1) : buf;
      }),
    );
    if (pieces.length === 1) {
      return { data: pieces[0]!, cacheControl: 'max-age=31536000' };
    }
    const total = pieces.reduce((n, p) => n + p.byteLength, 0);
    const out = new Uint8Array(total);
    let at = 0;
    for (const p of pieces) {
      out.set(new Uint8Array(p), at);
      at += p.byteLength;
    }
    return { data: out.buffer, cacheControl: 'max-age=31536000' };
  }
}

type Status = { key?: string; part_bytes?: number };

/** latestKey reads the key of the newest basemap from the status of base. */
export async function latestKey(base: string): Promise<{ key: string; partBytes: number }> {
  const res = await fetch(`${base}/status/latest.json`, { cache: 'no-store', credentials: 'omit' });
  if (!res.ok) {
    throw new Error(`The basemap status answered ${res.status}`);
  }
  const s = (await res.json()) as Status;
  if (!s.key) {
    throw new Error('The basemap status names no archive');
  }
  return { key: s.key, partBytes: s.part_bytes || PART_BYTES };
}

/** TERRAIN_URL is the hillshade archive the terrain shading draws from. */
export const TERRAIN_URL = `${TILES_BASE}/terrain/7c94bc6966ab-20260829/squallar-terrain-hillshade.pmtiles`;

/** TERRAIN_MAX_ZOOM is the deepest zoom the hillshade archive holds. */
export const TERRAIN_MAX_ZOOM = 12;

/** archiveSource probes url for parts, as the publisher's contract asks, and reads it either way, through the block cache. */
export async function archiveSource(url: string, partBytes: number): Promise<Source> {
  const res = await fetch(url + partName(0), { headers: { Range: 'bytes=0-6' }, credentials: 'omit' }).catch(() => null);
  const inner = res?.ok && new TextDecoder().decode(await res.arrayBuffer()).startsWith(MAGIC) ? new PartsSource(url, partBytes) : new FetchSource(url);
  return new BlockCachedSource(inner, generationOf(url));
}

/** clearOldArchives drops the cached blocks of every archive but basemap and the terrain. */
export async function clearOldArchives(basemap: string): Promise<void> {
  await BlockCachedSource.clearOtherGenerations([generationOf(basemap), generationOf(TERRAIN_URL)]);
}
