<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2026 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https://www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <BaseMap class="h-full w-full" @ready="onReady">
    <div
      v-if="card"
      class="pointer-events-none absolute z-10 flex flex-col gap-0.5 whitespace-nowrap rounded-lg border bg-background px-3 py-2 text-[13px] shadow-md"
      :style="card.left ? { right: `${card.width - card.x + 14}px`, top: `${card.y - 64}px` } : { left: `${card.x + 14}px`, top: `${card.y - 64}px` }"
      data-test="position-card"
    >
      <b class="font-semibold">{{ card.title }}</b>
      <span class="text-muted-foreground">{{ card.where }}</span>
      <span class="text-muted-foreground">{{ card.when }}</span>
    </div>
  </BaseMap>
</template>

<script setup lang="ts">
import type { GeoJSONSource, Map as MapLibre, MapLayerMouseEvent } from 'maplibre-gl';
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue';

import BaseMap from '@/components/map/BaseMap.vue';
import { formatAgo, formatCoordinates, type Position } from '@/services/positions';

export type MapTrack = {
  radio: number;
  color: string;
  title: string;
  points: Position[];
};

const props = defineProps<{ tracks: MapTrack[]; selected: number | null }>();
const emit = defineEmits<{ select: [radio: number] }>();

const SOURCE_TRACKS = 'positions-tracks';
const SOURCE_LATEST = 'positions-latest';
const CARD_ROOM = 260;

const map = shallowRef<MapLibre | null>(null);
const moved = ref(0);
let fitted = false;

const tracksData = () => ({
  type: 'FeatureCollection' as const,
  features: props.tracks
    .filter((t) => t.points.length > 1)
    .map((t) => ({
      type: 'Feature' as const,
      properties: { radio: t.radio, color: t.color },
      geometry: { type: 'LineString' as const, coordinates: t.points.map((p) => [p.longitude, p.latitude]) },
    })),
});

const latestData = () => ({
  type: 'FeatureCollection' as const,
  features: props.tracks
    .filter((t) => t.points.length > 0)
    .map((t) => {
      const p = t.points[t.points.length - 1]!;
      return {
        type: 'Feature' as const,
        properties: { radio: t.radio, color: t.color },
        geometry: { type: 'Point' as const, coordinates: [p.longitude, p.latitude] },
      };
    }),
});

function addLayers(m: MapLibre) {
  if (m.getSource(SOURCE_TRACKS)) {
    return;
  }
  m.addSource(SOURCE_TRACKS, { type: 'geojson', data: tracksData() });
  m.addSource(SOURCE_LATEST, { type: 'geojson', data: latestData() });
  m.addLayer({
    id: SOURCE_TRACKS,
    type: 'line',
    source: SOURCE_TRACKS,
    layout: { 'line-join': 'round', 'line-cap': 'round' },
    paint: { 'line-color': ['get', 'color'], 'line-width': 4, 'line-opacity': 0.85 },
  });
  m.addLayer({
    id: SOURCE_LATEST,
    type: 'circle',
    source: SOURCE_LATEST,
    paint: {
      'circle-color': ['get', 'color'],
      'circle-radius': 8,
      'circle-stroke-color': '#ffffff',
      'circle-stroke-width': 3,
    },
  });
}

function refresh() {
  const m = map.value;
  if (!m) {
    return;
  }
  (m.getSource(SOURCE_TRACKS) as GeoJSONSource | undefined)?.setData(tracksData());
  (m.getSource(SOURCE_LATEST) as GeoJSONSource | undefined)?.setData(latestData());
  fit();
}

function fit() {
  const m = map.value;
  const points = props.tracks.flatMap((t) => t.points);
  if (!m || fitted || points.length === 0) {
    return;
  }
  fitted = true;
  const lons = points.map((p) => p.longitude);
  const lats = points.map((p) => p.latitude);
  m.fitBounds([[Math.min(...lons), Math.min(...lats)], [Math.max(...lons), Math.max(...lats)]], {
    padding: 80,
    maxZoom: 14,
    duration: 0,
  });
}

const onMove = () => {
  moved.value++;
};

const onStyle = () => {
  if (map.value) {
    addLayers(map.value);
  }
};

const onClick = (e: MapLayerMouseEvent) => {
  const radio = e.features?.[0]?.properties?.radio;
  if (typeof radio === 'number') {
    emit('select', radio);
  }
};

function onReady(m: MapLibre) {
  map.value = m;
  addLayers(m);
  m.on('styledata', onStyle);
  m.on('move', onMove);
  m.on('click', SOURCE_LATEST, onClick);
  m.on('mouseenter', SOURCE_LATEST, () => {
    m.getCanvas().style.cursor = 'pointer';
  });
  m.on('mouseleave', SOURCE_LATEST, () => {
    m.getCanvas().style.cursor = '';
  });
  fit();
}

watch(() => props.tracks, refresh, { deep: true });

const card = computed(() => {
  void moved.value;
  const m = map.value;
  const t = props.tracks.find((x) => x.radio === props.selected);
  const p = t?.points[t.points.length - 1];
  if (!m || !t || !p) {
    return null;
  }
  const at = m.project([p.longitude, p.latitude]);
  const width = m.getContainer().clientWidth;
  const accuracy = p.accuracy_m !== null ? ` · ±${Math.round(p.accuracy_m)} m` : '';
  return {
    x: at.x,
    y: at.y,
    width,
    left: at.x > width - CARD_ROOM,
    title: t.title,
    where: formatCoordinates(p) + accuracy,
    when: `${formatAgo(p.received_at)} via ${p.repeater_id} TS${p.slot}`,
  };
});

onBeforeUnmount(() => {
  map.value?.off('styledata', onStyle);
  map.value?.off('move', onMove);
  map.value = null;
});
</script>
