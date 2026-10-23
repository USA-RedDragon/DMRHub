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

import { convertFilter, expression, type FilterSpecification } from '@maplibre/maplibre-gl-style-spec';
import type { LayerSpecification, StyleSpecification } from 'maplibre-gl';

import { TERRAIN_MAX_ZOOM } from './archive';
import { HILLSHADE_FADE_ZOOMS, hillshadeOpacity } from './hillshade';
import { TERRAIN_PROTOCOL, TERRAIN_SOURCE } from './terrain';

/** OMT_DEEPEST_ZOOM is the deepest zoom the OpenMapTiles archive holds; MapLibre overzooms past it. */
export const OMT_DEEPEST_ZOOM = 14;

/** FONTS maps each font of the styles to the glyphs DMRHub serves for it. */
export const FONTS: Record<string, string> = {
  'Montserrat Regular': 'Open Sans Regular',
  'Montserrat Medium': 'Open Sans Bold',
  'Montserrat Regular Italic': 'Open Sans Italic',
  'Montserrat Medium Italic': 'Open Sans Italic',
};

/** SOURCE is the id of the vector source in the committed styles. */
export const SOURCE = 'openmaptiles';

type Json = unknown;

function numeric(v: Json): v is number[] {
  return Array.isArray(v) && v.every((x) => typeof x === 'number');
}

/** strictExpression rewrites the loose forms the committed styles use into ones MapLibre accepts: a bare numeric array where an expression is expected becomes a literal. */
export function strictExpression(v: Json, top = true): Json {
  if (!Array.isArray(v)) {
    return v;
  }
  if (numeric(v)) {
    return top ? v : ['literal', v];
  }
  const [op, ...args] = v as [Json, ...Json[]];
  if (typeof op !== 'string' || op === 'literal') {
    return v;
  }
  if (op === 'match') {
    const out: Json[] = [op, strictExpression(args[0], false)];
    for (let i = 1; i < args.length; i++) {
      const label = i % 2 === 1 && i < args.length - 1;
      out.push(label ? args[i] : strictExpression(args[i], false));
    }
    return out;
  }
  return [op, ...args.map((a) => strictExpression(a, false))];
}

const COMBINATORS = new Set(['all', 'any', 'none']);

/** strictFilter turns a legacy filter into an expression, and a combinator that mixes the two into one of expressions only. */
export function strictFilter(f: Json): Json {
  if (!Array.isArray(f)) {
    return f;
  }
  const [op, ...children] = f as [string, ...Json[]];
  if (COMBINATORS.has(op) && children.some((c) => expression.isExpressionFilter(c))) {
    const parts = children.map((c) => strictFilter(c));
    return op === 'none' ? ['!', ['any', ...parts]] : [op, ...parts];
  }
  if (op === '!' && children.length === 1) {
    return ['!', strictFilter(children[0])];
  }
  if (expression.isExpressionFilter(f)) {
    return strictExpression(f);
  }
  return convertFilter(f as FilterSpecification);
}

/** basemapStyle points a committed style at the archive and the served glyphs. */
export function basemapStyle(raw: StyleSpecification, archive: string, glyphs: string): StyleSpecification {
  const style = structuredClone(raw);
  style.sources = { ...style.sources, [SOURCE]: { type: 'vector', url: `pmtiles://${archive}` } };
  style.glyphs = glyphs;
  for (const layer of style.layers) {
    const l = layer as { filter?: Json; paint?: Record<string, Json>; layout?: Record<string, Json> };
    if (l.filter) {
      l.filter = strictFilter(l.filter);
    }
    for (const group of [l.paint, l.layout]) {
      for (const k of Object.keys(group ?? {})) {
        group![k] = strictExpression(group![k]);
      }
    }
    if (layer.type !== 'symbol' || !layer.layout) {
      continue;
    }
    const font = layer.layout['text-font'];
    if (Array.isArray(font) && typeof font[0] === 'string') {
      layer.layout['text-font'] = [FONTS[font[0]] ?? 'Open Sans Regular'];
    }
  }
  return style;
}

/** TERRAIN_ATTRIBUTION credits the elevation data the hillshade is made from. */
export const TERRAIN_ATTRIBUTION = '© Copernicus DEM 2021';

/** TERRAIN_BELOW is the layer the terrain shading draws under: above land cover and land use, under the lines, water, roads and labels. */
export const TERRAIN_BELOW = 'boundary_county';

/** withTerrain adds the hillshade to a basemap style, fading out by zoom and drawn lighter on the light theme. */
export function withTerrain(style: StyleSpecification, dark: boolean): StyleSpecification {
  const layer = {
    id: TERRAIN_SOURCE,
    type: 'raster',
    source: TERRAIN_SOURCE,
    maxzoom: HILLSHADE_FADE_ZOOMS[1],
    paint: { 'raster-opacity': hillshadeOpacity(dark), 'raster-fade-duration': 0, 'raster-resampling': 'linear' },
  } as LayerSpecification;
  const at = style.layers.findIndex((l) => l.id === TERRAIN_BELOW);
  const layers = [...style.layers];
  layers.splice(at < 0 ? layers.length : at, 0, layer);
  return {
    ...style,
    sources: {
      ...style.sources,
      [TERRAIN_SOURCE]: { type: 'raster', tiles: [`${TERRAIN_PROTOCOL}://{z}/{x}/{y}`], tileSize: 256, minzoom: 0, maxzoom: TERRAIN_MAX_ZOOM, attribution: TERRAIN_ATTRIBUTION },
    },
    layers,
  };
}

/** circleImage draws the dot that the styles name circle-11 and ship no sprite for. */
export function circleImage(color: string): { width: number; height: number; data: Uint8ClampedArray } {
  const size = 22;
  const data = new Uint8ClampedArray(size * size * 4);
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(color.slice(i, i + 2), 16));
  const c = (size - 1) / 2;
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const d = Math.hypot(x - c, y - c);
      const a = Math.max(0, Math.min(1, c - d));
      const k = (y * size + x) * 4;
      data[k] = r!;
      data[k + 1] = g!;
      data[k + 2] = b!;
      data[k + 3] = Math.round(a * 255);
    }
  }
  return { width: size, height: size, data };
}
