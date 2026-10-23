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
    <div class="flex flex-col gap-1.5">
      <RouterLink to="/positions" class="text-sm text-primary underline-offset-4 hover:underline">Positions</RouterLink>
      <h1 class="text-2xl font-semibold">{{ radio }}</h1>
      <p v-if="state === 'ready'" class="text-sm text-muted-foreground" data-test="radio-subtitle">{{ subtitle }}</p>
    </div>

    <Card v-if="state === 'missing'" data-test="radio-missing">
      <CardHeader>
        <CardTitle>Not one of your radios</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-sm">Only the owner of a radio ID sets its position tracking.</p>
      </CardContent>
    </Card>

    <Card v-else-if="state === 'error'">
      <CardHeader>
        <CardTitle>Error</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-destructive">Could not load this radio.</p>
      </CardContent>
    </Card>

    <form v-else-if="state === 'ready'" class="grid items-start gap-6 lg:grid-cols-2" @submit.prevent="save">
      <Card>
        <CardHeader>
          <CardTitle>Position tracking</CardTitle>
          <CardDescription>DMRHub asks this radio for its position when it registers, and keeps the reports it sends.</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-5">
          <div class="flex items-center justify-between gap-4">
            <div class="flex flex-col gap-0.5">
              <span id="track-label" class="text-sm font-medium">Position tracking</span>
              <span class="text-[13px] text-muted-foreground">{{ form.tracking ? 'On.' : 'Off.' }}</span>
            </div>
            <Switch v-model="form.tracking" aria-labelledby="track-label" data-test="tracking-switch" />
          </div>
          <div class="flex flex-col gap-1.5">
            <label for="interval" class="text-sm font-medium">Report every</label>
            <select id="interval" v-model.number="form.interval_minutes" class="h-10 rounded-md border bg-background px-3 text-sm">
              <option v-for="m in choices.intervals" :key="m" :value="m">{{ m }} minutes</option>
            </select>
          </div>
          <div class="flex flex-col gap-1.5">
            <label for="keep" class="text-sm font-medium">Keep positions for</label>
            <select id="keep" v-model.number="form.retention_hours" class="h-10 rounded-md border bg-background px-3 text-sm">
              <option v-for="h in choices.retention_hours" :key="h" :value="h">{{ formatRetention(h) }}</option>
            </select>
          </div>
          <div class="flex flex-col gap-2">
            <span class="text-sm font-medium">Download the track</span>
            <div class="flex flex-wrap gap-2">
              <ShadButton
                v-for="f in downloads"
                :key="f.format"
                as="a"
                variant="outline"
                size="sm"
                :href="trackURL(radio, f.format)"
                download
                :data-test="`download-${f.format}`"
              >
                {{ f.label }}
              </ShadButton>
            </div>
          </div>
          <p class="max-w-[70ch] border-l-[3px] py-1 pl-3.5 text-sm">
            Only you see this radio's positions unless you share your positions. Turning tracking off deletes the
            positions kept for this radio.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>APRS-IS</CardTitle>
          <CardDescription>Send this radio's positions to APRS-IS under your callsign, at most once a minute.</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-5">
          <p v-if="!choices.aprs" class="text-sm text-muted-foreground" data-test="aprs-unavailable">
            This hub does not send positions to APRS-IS.
          </p>
          <div class="flex items-center justify-between gap-4">
            <div class="flex flex-col gap-0.5">
              <span id="aprs-label" class="text-sm font-medium">Send to APRS-IS</span>
              <span class="text-[13px] text-muted-foreground" data-test="aprs-status">{{ aprsStatus }}</span>
            </div>
            <Switch
              v-model="form.aprs"
              aria-labelledby="aprs-label"
              :disabled="!choices.aprs || !form.tracking"
              data-test="aprs-switch"
            />
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <div class="flex flex-col gap-1.5">
              <label for="ssid" class="text-sm font-medium">SSID</label>
              <select id="ssid" v-model="form.aprs_ssid" class="h-10 rounded-md border bg-background px-3 text-sm" :disabled="!choices.aprs">
                <option v-for="s in ssids" :key="s.value" :value="s.value">{{ s.label }}</option>
              </select>
            </div>
            <div class="flex flex-col gap-1.5">
              <label for="symbol" class="text-sm font-medium">Symbol</label>
              <select id="symbol" v-model="form.aprs_symbol" class="h-10 rounded-md border bg-background px-3 text-sm" :disabled="!choices.aprs">
                <option value="">{{ reportedSymbol }}</option>
                <option v-for="s in SYMBOLS" :key="s.name" :value="s.name">{{ s.label }}</option>
              </select>
            </div>
          </div>
          <div class="flex flex-col gap-1.5">
            <label for="comment" class="text-sm font-medium">Comment</label>
            <ShadInput id="comment" v-model="form.aprs_comment" maxlength="43" :disabled="!choices.aprs" />
          </div>
          <div role="note" class="rounded-md border bg-muted/40 px-3.5 py-3 text-sm">
            APRS-IS shows positions to anyone. Positions the radio has not moved from are not sent again.
          </div>
          <div class="flex items-center gap-3">
            <ShadButton type="submit" :disabled="saving" data-test="save">Save</ShadButton>
            <span v-if="message" class="text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'" data-test="save-message">
              {{ message }}
            </span>
          </div>
        </CardContent>
      </Card>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { RouterLink, useRoute } from 'vue-router';
import { useHead } from '@unhead/vue';

import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import {
  aprsName,
  formatAgo,
  formatRetention,
  getPositionRadios,
  getRadioPositions,
  serviceSymbol,
  setPositionRadio,
  SSIDS,
  symbolLabel,
  SYMBOLS,
  trackURL,
  type Position,
  type PositionRadioPut,
  type PositionRadios,
} from '@/services/positions';
import { errorStatus, errorText } from '@/services/settings';

const route = useRoute();
const radio = Number(route.params.id);

useHead({ title: `Positions of ${radio}` });

const downloads = [
  { format: 'gpx', label: 'GPX' },
  { format: 'geojson', label: 'GeoJSON' },
  { format: 'csv', label: 'CSV' },
] as const;

const state = ref<'loading' | 'ready' | 'missing' | 'error'>('loading');
const choices = ref<Pick<PositionRadios, 'intervals' | 'retention_hours' | 'aprs' | 'callsign'>>({
  intervals: [],
  retention_hours: [],
  aprs: false,
  callsign: '',
});
const latest = ref<Position | null>(null);
const form = reactive<PositionRadioPut>({
  tracking: false,
  interval_minutes: 10,
  retention_hours: 24,
  aprs: false,
  aprs_ssid: '9',
  aprs_symbol: '',
  aprs_comment: '',
});
const saving = ref(false);
const message = ref('');
const failed = ref(false);

const ssids = computed(() =>
  SSIDS.some((s) => s.value === form.aprs_ssid) ? SSIDS : [...SSIDS, { value: form.aprs_ssid, label: `-${form.aprs_ssid}` }],
);

const subtitle = computed(() => {
  const when = latest.value ? `last report ${formatAgo(latest.value.received_at)}` : 'no report in the last day';
  return `${choices.value.callsign} · ${when}`;
});

const reportedSymbol = computed(() => {
  const name = latest.value ? serviceSymbol(latest.value.service_id) : undefined;
  return name ? `As the radio reports it (${symbolLabel(name).toLowerCase()})` : 'As the radio reports it';
});

const aprsStatus = computed(() =>
  form.aprs && form.tracking ? `On, as ${aprsName(choices.value.callsign, form.aprs_ssid)}.` : 'Off.',
);

watch(
  () => form.tracking,
  (on) => {
    if (!on) {
      form.aprs = false;
    }
  },
);

const save = async () => {
  saving.value = true;
  message.value = '';
  failed.value = false;
  try {
    const s = await setPositionRadio(radio, { ...form });
    Object.assign(form, {
      tracking: s.tracking,
      interval_minutes: s.interval_minutes,
      retention_hours: s.retention_hours,
      aprs: s.aprs,
      aprs_ssid: s.aprs_ssid,
      aprs_symbol: s.aprs_symbol,
      aprs_comment: s.aprs_comment,
    });
    message.value = 'Saved.';
  } catch (err) {
    failed.value = true;
    message.value = errorText(err, 'Could not save. Try again later.');
  } finally {
    saving.value = false;
  }
};

onMounted(async () => {
  try {
    const r = await getPositionRadios();
    const s = r.radios.find((x) => x.radio_id === radio);
    if (!s) {
      state.value = 'missing';
      return;
    }
    choices.value = { intervals: r.intervals, retention_hours: r.retention_hours, aprs: r.aprs, callsign: r.callsign };
    Object.assign(form, {
      tracking: s.tracking,
      interval_minutes: s.interval_minutes,
      retention_hours: s.retention_hours,
      aprs: s.aprs,
      aprs_ssid: s.aprs_ssid || (s.aprs ? '' : '9'),
      aprs_symbol: s.aprs_symbol,
      aprs_comment: s.aprs_comment,
    });
    if (!r.intervals.includes(form.interval_minutes)) {
      form.interval_minutes = r.intervals.find((m) => m >= form.interval_minutes) ?? r.intervals[r.intervals.length - 1] ?? 10;
    }
    if (!r.retention_hours.includes(form.retention_hours)) {
      form.retention_hours = r.retention_hours[0] ?? 24;
    }
    try {
      latest.value = (await getRadioPositions(radio, 24)).latest;
    } catch (err) {
      if (errorStatus(err) !== 404) {
        throw err;
      }
    }
    state.value = 'ready';
  } catch (err) {
    state.value = errorStatus(err) === 404 ? 'missing' : 'error';
  }
});
</script>
