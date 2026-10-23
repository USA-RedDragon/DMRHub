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

import type { StyleSpecification } from 'maplibre-gl';
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?url';
import { PMTiles, Protocol } from 'pmtiles';

import { archiveSource, clearOldArchives, latestKey, TILES_BASE } from './archive';
import { basemapStyle, withTerrain } from './style';
import { TERRAIN_PROTOCOL, terrainTile } from './terrain';

/** BASEMAP_ATTRIBUTION credits the basemap's data; the terrain source credits its own. */
export const BASEMAP_ATTRIBUTION = '© OpenMapTiles © OpenStreetMap contributors';

type Prepared = { maplibregl: typeof import('maplibre-gl'); archive: string };

let prepared: Promise<Prepared> | null = null;

/** prepareMap loads MapLibre, finds the newest basemap, and registers the archive protocols, once per page. */
export function prepareMap(): Promise<Prepared> {
  if (!prepared) {
    prepared = (async () => {
      const maplibregl = await import('maplibre-gl');
      maplibregl.setWorkerUrl(workerUrl);
      const { key, partBytes } = await latestKey(TILES_BASE);
      const archive = `${TILES_BASE}/${key}`;
      const protocol = new Protocol();
      protocol.add(new PMTiles(await archiveSource(archive, partBytes)));
      maplibregl.addProtocol('pmtiles', protocol.tile);
      maplibregl.addProtocol(TERRAIN_PROTOCOL, terrainTile);
      void clearOldArchives(archive).catch(() => undefined);
      return { maplibregl, archive };
    })();
    prepared.catch(() => {
      prepared = null;
    });
  }
  return prepared;
}

/** mapStyle is the basemap style for a theme, pointed at archive, with the terrain shading. */
export async function mapStyle(archive: string, dark: boolean): Promise<StyleSpecification> {
  const raw = (dark ? await import('./styles/dark.json') : await import('./styles/light.json')).default as unknown as StyleSpecification;
  return withTerrain(basemapStyle(raw, archive, `${location.origin}/glyphs/{fontstack}/{range}.pbf`), dark);
}
