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
  <Card v-if="calls.length > 0" data-test="on-air">
    <CardHeader>
      <CardTitle>On the air</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <div class="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Slot</TableHead>
              <TableHead>From</TableHead>
              <TableHead>To</TableHead>
              <TableHead>Length</TableHead>
              <TableHead><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="c in calls" :key="c.call_id" :data-test="`on-air-${c.call_id}`">
              <TableCell>TS{{ c.slot }}</TableCell>
              <TableCell>
                {{ c.callsign || c.source_id }}
                <span v-if="c.callsign" class="font-mono text-xs text-muted-foreground">{{ c.source_id }}</span>
              </TableCell>
              <TableCell>{{ c.group_call ? `TG ${c.destination_id}` : c.destination_id }}</TableCell>
              <TableCell class="font-mono">{{ length(c.start_time) }}</TableCell>
              <TableCell class="text-right">
                <ShadButton
                  variant="outline"
                  size="sm"
                  class="text-destructive"
                  :disabled="dropping === c.call_id"
                  :data-test="`drop-${c.call_id}`"
                  @click="drop(c)"
                >Drop call</ShadButton>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
      <p v-if="message" class="text-sm text-destructive" data-test="on-air-message">{{ message }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import API from '@/services/API';
import { errorText } from '@/services/settings';
import { callLength, type OnAirCall } from '@/lib/sysop';

const props = defineProps<{ repeaterId: number }>();
const emit = defineEmits<{ calls: [calls: OnAirCall[]] }>();

const REFRESH_MS = 2_000;

const calls = ref<OnAirCall[]>([]);
const now = ref(Date.now());
const dropping = ref(0);
const message = ref('');
let timer: ReturnType<typeof setInterval> | null = null;

const length = (start: string) => callLength(start, now.value);

const load = async () => {
  now.value = Date.now();
  try {
    const res = await API.get(`/repeaters/${props.repeaterId}/on-air`);
    calls.value = Array.isArray(res.data?.calls) ? res.data.calls : [];
    emit('calls', calls.value);
  } catch (err) {
    console.error(err);
  }
};

const drop = async (c: OnAirCall) => {
  dropping.value = c.call_id;
  message.value = '';
  try {
    await API.post(`/repeaters/${props.repeaterId}/drop-call`, { call_id: c.call_id });
    calls.value = calls.value.filter((x) => x.call_id !== c.call_id);
    emit('calls', calls.value);
  } catch (err) {
    message.value = errorText(err, 'Could not drop the call. Try again.');
  } finally {
    dropping.value = 0;
  }
};

onMounted(() => {
  load();
  timer = setInterval(load, REFRESH_MS);
});

onUnmounted(() => {
  if (timer !== null) clearInterval(timer);
});
</script>
