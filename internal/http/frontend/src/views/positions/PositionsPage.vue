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
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-end justify-between gap-4">
      <div class="flex flex-col gap-1.5">
        <h1 class="text-2xl font-semibold">Positions</h1>
        <p class="text-sm text-muted-foreground">Where your radios last reported from, and their tracks.</p>
      </div>
      <div v-if="state === 'ready'" class="flex flex-wrap items-center gap-2">
        <span class="text-sm text-muted-foreground">Show</span>
        <div role="group" aria-label="Track window" class="flex overflow-hidden rounded-lg border">
          <button
            v-for="(w, i) in WINDOWS"
            :key="w.hours"
            type="button"
            class="px-3.5 py-2 text-sm"
            :class="[hours === w.hours ? 'bg-primary text-primary-foreground' : 'bg-background', i > 0 ? 'border-l' : '']"
            :aria-pressed="hours === w.hours"
            :data-test="`window-${w.hours}`"
            @click="setWindow(w.hours)"
          >
            {{ w.label }}
          </button>
        </div>
        <label class="ml-3 flex items-center gap-2 text-sm">
          <input v-model="showShared" type="checkbox" class="h-4 w-4" data-test="show-shared" />
          Show shared radios
        </label>
      </div>
    </div>

    <Card v-if="state === 'anon'" data-test="positions-anon">
      <CardHeader>
        <CardTitle>Log in to see your positions</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <p class="text-sm">Positions are private to the owner of each radio unless they share them.</p>
        <div>
          <ShadButton as-child>
            <RouterLink to="/login">Login</RouterLink>
          </ShadButton>
        </div>
      </CardContent>
    </Card>

    <Card v-else-if="state === 'error'">
      <CardHeader>
        <CardTitle>Error</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-destructive">Could not load your positions.</p>
      </CardContent>
    </Card>

    <div v-else-if="state === 'ready'" class="grid gap-6 lg:grid-cols-5">
      <div class="relative h-[55vh] min-h-[360px] overflow-hidden rounded-xl border lg:col-span-3 lg:h-[70vh]">
        <PositionsMap :tracks="mapTracks" :selected="selected" @select="(r) => (selected = r)" />
      </div>
      <Card class="lg:col-span-2">
        <CardHeader>
          <CardTitle>Radios</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <p v-if="rows.length === 0" class="text-sm text-muted-foreground" data-test="no-radios">
            You have no radio IDs yet. Add them in your settings.
          </p>
          <ul class="flex flex-col">
            <li
              v-for="row in rows"
              :key="row.radio"
              class="flex cursor-pointer items-start gap-3 border-b py-3 last:border-b-0"
              :class="{ 'bg-muted/50': selected === row.radio }"
              :data-test="`radio-${row.radio}`"
              @click="selected = row.radio"
            >
              <span
                aria-hidden="true"
                class="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full"
                :style="row.tracking || row.shared ? { background: row.color } : { border: '2px solid #a1a1aa' }"
              />
              <div class="flex min-w-0 grow flex-col gap-0.5">
                <div class="flex justify-between gap-2">
                  <b class="text-sm font-semibold">{{ row.title }}</b>
                  <span v-if="row.latest" class="text-[13px] text-muted-foreground">{{ formatAgo(row.latest.received_at, now) }}</span>
                </div>
                <span v-if="row.latest" class="text-[13px] text-muted-foreground">{{ summary(row.latest) }}</span>
                <span class="text-[13px] text-muted-foreground">{{ row.status }}</span>
              </div>
              <RouterLink
                v-if="!row.shared"
                :to="`/positions/${row.radio}`"
                class="text-[13px] text-primary underline-offset-4 hover:underline"
                @click.stop
              >
                Settings
              </RouterLink>
            </li>
          </ul>
          <div class="mt-auto flex items-center justify-between gap-3 border-t pt-4">
            <div class="flex flex-col gap-0.5">
              <span id="share-label" class="text-sm font-medium">Share my positions</span>
              <span class="text-[13px] text-muted-foreground">Every logged-in user sees your radios on this map.</span>
            </div>
            <Switch
              aria-labelledby="share-label"
              :model-value="share"
              :disabled="saving"
              data-test="share-switch"
              @update:model-value="onShare"
            />
          </div>
          <p v-if="shareError" class="text-sm text-destructive">{{ shareError }}</p>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { useHead } from '@unhead/vue';

import PositionsMap, { type MapTrack } from '@/components/map/PositionsMap.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import {
  aprsName,
  formatAgo,
  getPositionRadios,
  getPositions,
  getRadioPositions,
  isPositionPush,
  summary,
  WINDOWS,
  type Position,
  type PositionRadios,
} from '@/services/positions';
import { errorStatus, errorText, getSettings, setSharePositions } from '@/services/settings';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

useHead({ title: 'Positions' });

const OWN_COLORS = ['#2563eb', '#ea580c', '#16a34a', '#9333ea', '#db2777', '#0891b2', '#ca8a04'];
const SHARED_COLOR = '#52525b';

type Row = {
  radio: number;
  title: string;
  color: string;
  shared: boolean;
  tracking: boolean;
  latest: Position | null;
  status: string;
};

const state = ref<'loading' | 'ready' | 'anon' | 'error'>('loading');
const hours = ref(24);
const showShared = ref(true);
const radios = ref<PositionRadios | null>(null);
const tracks = ref<Record<number, Position[]>>({});
const share = ref(false);
const saving = ref(false);
const shareError = ref('');
const selected = ref<number | null>(null);
const now = ref(Date.now());
let socket: { close(): void } | null = null;
let clock: ReturnType<typeof setInterval> | null = null;
let alive = true;

const latestOf = (radio: number): Position | null => {
  const t = tracks.value[radio];
  return t && t.length > 0 ? t[t.length - 1]! : null;
};

const rows = computed<Row[]>(() => {
  const r = radios.value;
  if (!r) {
    return [];
  }
  const own: Row[] = r.radios.map((s, i) => {
    let status = 'Tracking is off';
    if (s.tracking) {
      status = `Tracking every ${s.interval_minutes} min`;
      if (s.aprs) {
        status += ` · APRS-IS as ${aprsName(r.callsign, s.aprs_ssid)}`;
      }
    }
    return {
      radio: s.radio_id,
      title: String(s.radio_id),
      color: OWN_COLORS[i % OWN_COLORS.length]!,
      shared: false,
      tracking: s.tracking,
      latest: latestOf(s.radio_id),
      status,
    };
  });
  if (!showShared.value) {
    return own;
  }
  const mine = new Set(r.radios.map((s) => s.radio_id));
  const shared: Row[] = Object.keys(tracks.value)
    .map(Number)
    .filter((radio) => !mine.has(radio))
    .map((radio) => latestOf(radio))
    .filter((p): p is Position => p !== null && p.shared)
    .sort((a, b) => a.radio_id - b.radio_id)
    .map((p) => ({
      radio: p.radio_id,
      title: p.callsign ? `${p.radio_id} · ${p.callsign}` : String(p.radio_id),
      color: SHARED_COLOR,
      shared: true,
      tracking: true,
      latest: p,
      status: p.callsign ? `Shared by ${p.callsign}` : 'Shared',
    }));
  return [...own, ...shared];
});

const mapTracks = computed<MapTrack[]>(() =>
  rows.value
    .filter((row) => row.latest)
    .map((row) => ({
      radio: row.radio,
      color: row.color,
      title: row.shared ? row.title : `${radios.value?.callsign ?? ''} · ${row.radio}`,
      points: tracks.value[row.radio] ?? [],
    })),
);

const loadTracks = async () => {
  const latest = await getPositions(hours.value);
  const next: Record<number, Position[]> = {};
  await Promise.all(
    latest.map(async (p) => {
      const r = await getRadioPositions(p.radio_id, hours.value);
      next[p.radio_id] = r.track.length > 0 ? r.track : [p];
    }),
  );
  tracks.value = next;
};

const setWindow = async (h: number) => {
  if (h === hours.value) {
    return;
  }
  hours.value = h;
  try {
    await loadTracks();
  } catch {
    state.value = 'error';
  }
};

const onShare = async (value: boolean) => {
  saving.value = true;
  shareError.value = '';
  try {
    const s = await setSharePositions(value);
    share.value = !!s.share_positions;
    await loadTracks();
  } catch (err) {
    shareError.value = errorText(err, 'Could not change sharing. Try again later.');
  } finally {
    saving.value = false;
  }
};

const onSocketMessage = (event: MessageEvent) => {
  let frame: unknown;
  try {
    frame = JSON.parse(event.data);
  } catch {
    return;
  }
  if (!isPositionPush(frame)) {
    return;
  }
  const p = frame.position;
  const track = tracks.value[p.radio_id] ?? [];
  if (track.some((x) => x.received_at === p.received_at)) {
    return;
  }
  tracks.value = { ...tracks.value, [p.radio_id]: [...track, p] };
};

const load = async () => {
  try {
    const [r, settings] = await Promise.all([getPositionRadios(), getSettings()]);
    radios.value = r;
    share.value = !!settings.share_positions;
    await loadTracks();
  } catch (err) {
    state.value = errorStatus(err) === 401 ? 'anon' : 'error';
    return;
  }
  if (!alive) {
    return;
  }
  state.value = 'ready';
  clock = setInterval(() => {
    now.value = Date.now();
  }, 30000);
  socket = ws.connect(getWebsocketURI() + '/messages', onSocketMessage);
};

onMounted(load);

onUnmounted(() => {
  alive = false;
  if (clock !== null) {
    clearInterval(clock);
  }
  if (socket) {
    socket.close();
  }
});
</script>
