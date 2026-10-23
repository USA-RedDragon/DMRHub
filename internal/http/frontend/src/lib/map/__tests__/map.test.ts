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
import { validateStyleMin } from '@maplibre/maplibre-gl-style-spec';
import type { StyleSpecification } from 'maplibre-gl';

import { archiveSource, latestKey, PartsSource, partName, spans } from '@/lib/map/archive';
import { BlockCachedSource } from '@/lib/map/blockCache';
import { basemapStyle, circleImage, FONTS, SOURCE, strictExpression, strictFilter, TERRAIN_ATTRIBUTION, TERRAIN_BELOW, withTerrain } from '@/lib/map/style';
import { TERRAIN_SOURCE } from '@/lib/map/terrain';
import dark from '@/lib/map/styles/dark.json';
import light from '@/lib/map/styles/light.json';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('archive parts', () => {
  it('names parts with three digits', () => {
    expect(partName(0)).toBe('.part000');
    expect(partName(167)).toBe('.part167');
  });

  it('maps a range onto the parts it covers', () => {
    expect(spans(0, 127, 1000)).toEqual([{ part: 0, start: 0, end: 126 }]);
    expect(spans(990, 20, 1000)).toEqual([
      { part: 0, start: 990, end: 999 },
      { part: 1, start: 0, end: 9 },
    ]);
    expect(spans(2500, 10, 1000)).toEqual([{ part: 2, start: 500, end: 509 }]);
  });

  it('reads a range across a part boundary in order', async () => {
    const fetch = vi.fn((url: string, init: RequestInit) => {
      const range = (init.headers as Record<string, string>).Range!;
      const [a, b] = range.replace('bytes=', '').split('-').map(Number) as [number, number];
      const part = Number(url.slice(-3));
      const bytes = Uint8Array.from({ length: b - a + 1 }, (_, i) => (part * 10 + a + i) % 256);
      return Promise.resolve(new Response(bytes, { status: 206 }));
    });
    vi.stubGlobal('fetch', fetch);
    const src = new PartsSource('https://tiles.example/basemap/x.pmtiles', 10);
    const got = new Uint8Array((await src.getBytes(8, 4)).data);
    expect([...got]).toEqual([8, 9, 10, 11]);
    expect(fetch.mock.calls.map((c) => c[0])).toEqual(['https://tiles.example/basemap/x.pmtiles.part000', 'https://tiles.example/basemap/x.pmtiles.part001']);
  });

  it('probes part000 for the PMTiles magic', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(new TextEncoder().encode('PMTiles'), { status: 206 })));
    const parts = await archiveSource('https://tiles.example/a.pmtiles', 10);
    expect(parts).toBeInstanceOf(BlockCachedSource);
    expect((parts as BlockCachedSource).inner).toBeInstanceOf(PartsSource);
    expect((parts as BlockCachedSource).generation).toBe('a.pmtiles');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('nope', { status: 404 })));
    expect(((await archiveSource('https://tiles.example/a.pmtiles', 10)) as BlockCachedSource).inner).not.toBeInstanceOf(PartsSource);
  });

  it('reads the newest key from the status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ key: 'basemap/omt-1.pmtiles', part_bytes: 42 }))));
    expect(await latestKey('https://tiles.example')).toEqual({ key: 'basemap/omt-1.pmtiles', partBytes: 42 });
  });
});

describe('basemap style', () => {
  for (const [name, raw] of [
    ['light', light],
    ['dark', dark],
  ] as const) {
    it(`points ${name} at the archive and the served fonts`, () => {
      const s = basemapStyle(raw as unknown as StyleSpecification, 'https://tiles.example/a.pmtiles', '/glyphs/{fontstack}/{range}.pbf');
      expect(s.sources[SOURCE]).toEqual({ type: 'vector', url: 'pmtiles://https://tiles.example/a.pmtiles' });
      expect(s.glyphs).toBe('/glyphs/{fontstack}/{range}.pbf');
      expect(s.layers.length).toBe((raw as unknown as StyleSpecification).layers.length);
      const fonts = new Set(s.layers.flatMap((l) => (l.type === 'symbol' && Array.isArray(l.layout?.['text-font']) ? (l.layout['text-font'] as string[]) : [])));
      expect([...fonts].every((f) => Object.values(FONTS).includes(f))).toBe(true);
      expect((raw as unknown as StyleSpecification).sources[SOURCE]).not.toHaveProperty('url', 'pmtiles://https://tiles.example/a.pmtiles');
      expect(validateStyleMin(s).map((e) => e.message)).toEqual([]);
    });
  }

  it('makes the loose expressions strict', () => {
    expect(strictExpression(['step', ['zoom'], [1], 7, [2, 2]])).toEqual(['step', ['zoom'], ['literal', [1]], 7, ['literal', [2, 2]]]);
    expect(strictExpression([0.2, 0.2])).toEqual([0.2, 0.2]);
    expect(strictExpression(['match', ['get', 'rank'], [1, 2], 'a', 3, 'b', 'c'])).toEqual(['match', ['get', 'rank'], [1, 2], 'a', 3, 'b', 'c']);
  });

  it('makes legacy and mixed filters expressions', () => {
    expect(strictFilter(['all', ['!has', 'capital'], ['==', ['get', 'class'], 'city']])).toEqual(['all', ['!', ['has', 'capital']], ['==', ['get', 'class'], 'city']]);
    expect(strictFilter(['==', ['get', 'class'], 'city'])).toEqual(['==', ['get', 'class'], 'city']);
    const legacy = strictFilter(['all', ['==', 'class', 'service'], ['in', 'brunnel', 'bridge', 'tunnel']]);
    expect(expressionLike(legacy)).toBe(true);
  });

  it('puts the terrain under the county lines, fading out by zoom 15', () => {
    for (const [name, raw] of [
      ['dark', dark],
      ['light', light],
    ] as const) {
      const s = withTerrain(basemapStyle(raw as unknown as StyleSpecification, 'https://tiles.example/a.pmtiles', '/glyphs/{fontstack}/{range}.pbf'), name === 'dark');
      const ids = s.layers.map((l) => l.id);
      expect(ids.indexOf(TERRAIN_SOURCE)).toBe(ids.indexOf(TERRAIN_BELOW) - 1);
      expect(s.sources[TERRAIN_SOURCE]).toMatchObject({ type: 'raster', tiles: ['hillshade://{z}/{x}/{y}'], maxzoom: 12, attribution: TERRAIN_ATTRIBUTION });
      const layer = s.layers.find((l) => l.id === TERRAIN_SOURCE)!;
      expect(layer.maxzoom).toBe(15);
      expect(layer.paint).toMatchObject({ 'raster-opacity': ['interpolate', ['linear'], ['zoom'], 13, name === 'dark' ? 1 : 0.55, 15, 0] });
      expect(validateStyleMin(s).map((e) => e.message)).toEqual([]);
    }
  });

  it('draws a round dot for circle-11', () => {
    const img = circleImage('#ff0000');
    const at = (x: number, y: number) => img.data[(y * img.width + x) * 4 + 3];
    expect(at(11, 11)).toBe(255);
    expect(at(0, 0)).toBe(0);
    expect(img.data[(11 * img.width + 11) * 4]).toBe(255);
  });
});

function expressionLike(f: unknown): boolean {
  return Array.isArray(f) && JSON.stringify(f).includes('["get","class"]');
}
