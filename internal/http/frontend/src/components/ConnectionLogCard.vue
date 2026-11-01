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
  <Card data-test="connection-log">
    <CardHeader>
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="flex flex-col gap-1.5">
          <CardTitle>Connection log</CardTitle>
          <CardDescription>The last 24 hours.</CardDescription>
        </div>
        <label class="flex items-center gap-2 text-sm font-medium" for="log-problems">
          <Switch id="log-problems" v-model="problemsOnly" data-test="problems-only" />
          Problems only
        </label>
      </div>
    </CardHeader>
    <CardContent>
      <p v-if="loaded && shown.length === 0" class="text-sm text-muted-foreground" data-test="log-empty">
        {{ problemsOnly ? 'No problems in the last 24 hours.' : 'Nothing in the last 24 hours.' }}
      </p>
      <ol v-else class="divide-y">
        <li v-for="line in shown" :key="line.id" class="grid gap-1 py-3 sm:grid-cols-[8rem_1fr]" :data-test="`log-${line.id}`">
          <span class="text-sm text-muted-foreground">{{ line.at }}</span>
          <div class="flex flex-col gap-1">
            <p class="text-sm">
              <span class="mr-2 inline-block size-2 rounded-full align-middle" :class="dot(line.tone)" aria-hidden="true" />
              <strong>{{ line.title }}</strong>
              <span v-if="line.note" class="ml-1 text-xs text-muted-foreground">{{ line.note }}</span>
            </p>
            <p v-if="line.fix" class="text-sm text-muted-foreground">{{ line.fix }}</p>
          </div>
        </li>
      </ol>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import API from '@/services/API';
import { logLine, type LogLine, type RepeaterEvent } from '@/lib/connectionLog';

const props = defineProps<{ repeaterId: number; device: string }>();

const REFRESH_MS = 30_000;

const events = ref<RepeaterEvent[]>([]);
const loaded = ref(false);
const problemsOnly = ref(false);
let timer: ReturnType<typeof setInterval> | null = null;

const lines = computed<LogLine[]>(() => events.value.map((ev) => logLine(ev, props.repeaterId, props.device)));
const shown = computed(() => (problemsOnly.value ? lines.value.filter((l) => l.problem) : lines.value));

const dot = (tone: LogLine['tone']) => {
  if (tone === 'bad') return 'bg-destructive';
  if (tone === 'good') return 'bg-green-600';
  return 'bg-muted-foreground';
};

const load = async () => {
  try {
    const res = await API.get(`/repeaters/${props.repeaterId}/events`);
    events.value = Array.isArray(res.data?.events) ? res.data.events : [];
    loaded.value = true;
  } catch (err) {
    console.error(err);
  }
};

defineExpose({ load });

onMounted(() => {
  load();
  timer = setInterval(load, REFRESH_MS);
});

onUnmounted(() => {
  if (timer !== null) clearInterval(timer);
});
</script>
