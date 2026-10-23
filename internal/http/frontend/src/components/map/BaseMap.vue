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
  <div class="relative min-h-0 overflow-hidden bg-background">
    <div ref="el" style="position: absolute; inset: 0" />
    <p v-if="failure" class="absolute inset-x-0 top-4 mx-auto w-fit rounded-md border bg-background px-3 py-2 text-sm text-muted-foreground">
      The map could not load: {{ failure }}
    </p>
    <slot :map="map" />
  </div>
</template>

<script setup lang="ts">
import 'maplibre-gl/dist/maplibre-gl.css';

import type { Map as MapLibre } from 'maplibre-gl';
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue';

import { useColorMode } from '@/composables/useColorMode';
import { BASEMAP_ATTRIBUTION, mapStyle, prepareMap } from '@/lib/map/setup';
import { circleImage } from '@/lib/map/style';

const props = withDefaults(defineProps<{ center?: [number, number]; zoom?: number }>(), {
  center: () => [-96, 38],
  zoom: 3,
});
const emit = defineEmits<{ ready: [map: MapLibre] }>();

const { isDark } = useColorMode();
const el = ref<HTMLDivElement | null>(null);
const map = shallowRef<MapLibre | null>(null);
const failure = ref('');
let archive = '';

onMounted(async () => {
  try {
    const { maplibregl, archive: url } = await prepareMap();
    archive = url;
    if (!el.value) {
      return;
    }
    const dark = isDark.value;
    const m = new maplibregl.Map({
      container: el.value,
      style: await mapStyle(archive, dark),
      center: props.center,
      zoom: props.zoom,
      attributionControl: false,
      dragRotate: false,
      pitchWithRotate: false,
    });
    m.touchZoomRotate.disableRotation();
    m.addControl(new maplibregl.AttributionControl({ compact: true, customAttribution: BASEMAP_ATTRIBUTION }), 'bottom-left');
    m.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'bottom-right');
    m.setMissingStyleImageResolver((id) => {
      if (id === 'circle-11' && !m.hasImage(id)) {
        m.addImage(id, circleImage(isDark.value ? '#c9cbd1' : '#5a5f68'));
      }
    });
    map.value = m;
    m.once('load', () => {
      el.value?.querySelector('.maplibregl-ctrl-attrib')?.classList.remove('maplibregl-compact-show');
      emit('ready', m);
    });
    if (dark !== isDark.value) {
      m.setStyle(await mapStyle(archive, isDark.value));
    }
  } catch (err) {
    failure.value = err instanceof Error ? err.message : String(err);
  }
});

onBeforeUnmount(() => {
  map.value?.remove();
  map.value = null;
});

watch(isDark, async (dark) => {
  if (map.value && archive) {
    map.value.setStyle(await mapStyle(archive, dark));
  }
});

defineExpose({ map });
</script>
