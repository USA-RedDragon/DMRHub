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
import type { RangeResponse, Source } from 'pmtiles';

/** BLOCK_BYTES is the size of one cached block of an archive, as Squallar caches them. */
export const BLOCK_BYTES = 64 * 1024;

/** CACHE_MAX_BYTES is how many bytes of blocks the cache keeps of each generation before it drops the least recently used. */
export const CACHE_MAX_BYTES = 256 * 1024 * 1024;

/** STORAGE_FLOOR_BYTES is how much of the origin's quota the cache leaves free. */
export const STORAGE_FLOOR_BYTES = 64 * 1024 * 1024;

const PREFIX = 'dmrhub-blk-';
const META = 'dmrhub-blk-meta';
const MANIFEST_PREFIX = '/__dmrhub_blk_manifest__';

type Entry = { cache: string; bytes: number; touched: number };
type Manifest = { clock: number; blocks: Record<string, Entry> };

/** generationOf names the generation of an archive from its URL path, which changes with every publish. */
export function generationOf(url: string): string {
  try {
    return new URL(url).pathname.replace(/^\/+/, '');
  } catch {
    return url;
  }
}

/** cacheNameOf is the Cache Storage name that holds the blocks of generation. */
export function cacheNameOf(generation: string): string {
  return PREFIX + encodeURIComponent(generation);
}

/** blockSpan is the first and last block that bytes offset to offset+length-1 touch. */
export function blockSpan(offset: number, length: number): { first: number; last: number } {
  return { first: Math.floor(offset / BLOCK_BYTES), last: Math.floor((offset + length - 1) / BLOCK_BYTES) };
}

/** missingRuns groups the blocks from first to last that have is false into runs of neighbors, to fetch each run at once. */
export function missingRuns(first: number, last: number, have: (index: number) => boolean): Array<[number, number]> {
  const runs: Array<[number, number]> = [];
  for (let i = first; i <= last; i++) {
    if (have(i)) {
      continue;
    }
    const prev = runs[runs.length - 1];
    if (prev && prev[1] === i - 1) {
      prev[1] = i;
    } else {
      runs.push([i, i]);
    }
  }
  return runs;
}

/** evictionOrder lists the blocks to drop, least recently used first, until the rest fit in cap bytes. */
export function evictionOrder(manifest: Manifest, cap: number): string[] {
  let total = Object.values(manifest.blocks).reduce((n, e) => n + e.bytes, 0);
  const out: string[] = [];
  for (const [key, e] of Object.entries(manifest.blocks).sort((a, b) => a[1].touched - b[1].touched)) {
    if (total <= cap) {
      break;
    }
    out.push(key);
    total -= e.bytes;
  }
  return out;
}

function manifestKey(generation: string): string {
  return `${MANIFEST_PREFIX}/${encodeURIComponent(generation)}`;
}

function blockKey(generation: string, index: number): string {
  return `/__dmrhub_blk__/${encodeURIComponent(generation)}/${index}`;
}

/**
 * BlockCachedSource keeps the bytes an archive reader reads in the browser's Cache Storage, in aligned blocks of
 * BLOCK_BYTES, the way Squallar caches its archives. Archives never change once published, so a block is good
 * for as long as its generation is current, and only other generations are ever cleared. Any failure of the
 * cache falls back to reading through inner.
 */
export class BlockCachedSource implements Source {
  private readonly cacheName: string;
  private readonly pending = new Map<number, Promise<ArrayBuffer>>();
  private manifest: Promise<Manifest> | null = null;
  private saveTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(
    readonly inner: Source,
    readonly generation: string,
    private readonly store: CacheStorage | undefined = globalThis.caches,
  ) {
    this.cacheName = cacheNameOf(generation);
  }

  getKey(): string {
    return this.inner.getKey();
  }

  async getBytes(offset: number, length: number, signal?: AbortSignal, etag?: string): Promise<RangeResponse> {
    if (!this.store || length <= 0) {
      return this.inner.getBytes(offset, length, signal, etag);
    }
    try {
      return await this.cached(offset, length);
    } catch {
      return this.inner.getBytes(offset, length, signal, etag);
    }
  }

  /** clearOtherGenerations deletes the blocks of every generation but the ones in keep. */
  static async clearOtherGenerations(keep: string[], store: CacheStorage | undefined = globalThis.caches): Promise<void> {
    if (!store) {
      return;
    }
    const wanted = new Set([...keep.map(cacheNameOf), META]);
    const names = await store.keys();
    for (const name of names) {
      if (name.startsWith(PREFIX) && !wanted.has(name)) {
        await store.delete(name);
      }
    }
    if (!names.includes(META)) {
      return;
    }
    const manifests = new Set(keep.map(manifestKey));
    const meta = await store.open(META);
    for (const req of await meta.keys()) {
      if (!manifests.has(new URL(req.url, location.origin).pathname)) {
        await meta.delete(req);
      }
    }
  }

  private async cached(offset: number, length: number): Promise<RangeResponse> {
    const store = this.store!;
    const cache = await store.open(this.cacheName);
    const { first, last } = blockSpan(offset, length);
    const blocks = new Map<number, ArrayBuffer>();
    for (let i = first; i <= last; i++) {
      const hit = await cache.match(blockKey(this.generation, i));
      if (hit) {
        blocks.set(i, await hit.arrayBuffer());
        void this.touch(blockKey(this.generation, i));
      } else if (this.pending.has(i)) {
        blocks.set(i, await this.pending.get(i)!);
      }
    }
    for (const [from, to] of missingRuns(first, last, (i) => blocks.has(i))) {
      const run = this.fetchRun(cache, from, to);
      for (let i = from; i <= to; i++) {
        const one = run.then((all) => all[i - from] ?? new ArrayBuffer(0));
        this.pending.set(i, one);
        void one.finally(() => this.pending.delete(i)).catch(() => undefined);
      }
      const all = await run;
      all.forEach((b, k) => blocks.set(from + k, b));
    }
    const out = new Uint8Array(length);
    let filled = 0;
    for (let i = first; i <= last; i++) {
      const b = new Uint8Array(blocks.get(i) ?? new ArrayBuffer(0));
      const start = i * BLOCK_BYTES;
      const from = Math.max(offset - start, 0);
      const to = Math.min(offset + length - start, b.byteLength);
      if (to > from) {
        out.set(b.subarray(from, to), filled);
        filled += to - from;
      }
      if (b.byteLength < BLOCK_BYTES) {
        break;
      }
    }
    if (filled === 0) {
      throw new Error('range starts past the end of the archive');
    }
    return { data: (filled === length ? out : out.slice(0, filled)).buffer as ArrayBuffer, cacheControl: 'max-age=31536000' };
  }

  private async fetchRun(cache: Cache, from: number, to: number): Promise<ArrayBuffer[]> {
    const start = from * BLOCK_BYTES;
    const res = await this.inner.getBytes(start, (to - from + 1) * BLOCK_BYTES);
    const bytes = new Uint8Array(res.data);
    const out: ArrayBuffer[] = [];
    for (let i = from; i <= to; i++) {
      const a = (i - from) * BLOCK_BYTES;
      if (a >= bytes.byteLength && i > from) {
        break;
      }
      const block = bytes.slice(a, Math.min(a + BLOCK_BYTES, bytes.byteLength)).buffer as ArrayBuffer;
      out.push(block);
      void this.put(cache, blockKey(this.generation, i), block);
    }
    return out;
  }

  private async put(cache: Cache, key: string, block: ArrayBuffer): Promise<void> {
    try {
      if (await this.tight(block.byteLength)) {
        return;
      }
      await cache.put(key, new Response(block, { status: 200, headers: { 'content-type': 'application/octet-stream' } }));
      const m = await this.loadManifest();
      m.blocks[key] = { cache: this.cacheName, bytes: block.byteLength, touched: ++m.clock };
      for (const old of evictionOrder(m, CACHE_MAX_BYTES)) {
        const e = m.blocks[old]!;
        delete m.blocks[old];
        await (await this.store!.open(e.cache)).delete(old).catch(() => false);
      }
      this.saveSoon();
    } catch {
      return;
    }
  }

  private async tight(bytes: number): Promise<boolean> {
    try {
      const est = await navigator.storage?.estimate?.();
      return !!est?.quota && est.quota - (est.usage ?? 0) < bytes + STORAGE_FLOOR_BYTES;
    } catch {
      return false;
    }
  }

  private async touch(key: string): Promise<void> {
    try {
      const m = await this.loadManifest();
      const e = m.blocks[key];
      if (e) {
        e.touched = ++m.clock;
        this.saveSoon();
      }
    } catch {
      return;
    }
  }

  private loadManifest(): Promise<Manifest> {
    if (!this.manifest) {
      this.manifest = (async () => {
        const hit = await (await this.store!.open(META)).match(manifestKey(this.generation));
        const m = hit ? ((await hit.json().catch(() => null)) as Manifest | null) : null;
        return m && typeof m.blocks === 'object' && m.blocks !== null && Number.isSafeInteger(m.clock) ? m : { clock: 0, blocks: {} };
      })();
    }
    return this.manifest;
  }

  private saveSoon(): void {
    clearTimeout(this.saveTimer);
    this.saveTimer = setTimeout(() => {
      void (async () => {
        try {
          const m = await this.loadManifest();
          await (await this.store!.open(META)).put(manifestKey(this.generation), new Response(JSON.stringify(m), { headers: { 'content-type': 'application/json' } }));
        } catch {
          return;
        }
      })();
    }, 2000);
  }
}
