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
  <Card data-test="store-forward-card">
    <CardHeader>
      <CardTitle>Store and forward</CardTitle>
      <CardDescription>
        When your radio is off the air, DMRHub keeps the text messages sent to it and sends them when your radio is
        heard again.
      </CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div class="flex items-start justify-between gap-4">
        <div class="flex flex-col gap-1">
          <span id="store-forward-label" class="font-medium">Store and forward</span>
          <p class="text-sm" :class="on ? '' : 'text-muted-foreground'" data-test="store-forward-status">
            {{ on ? 'On.' : 'Off.' }}
          </p>
        </div>
        <Switch
          id="store-forward"
          aria-labelledby="store-forward-label"
          :model-value="on"
          :disabled="saving"
          @update:model-value="onSwitch"
        />
      </div>
      <p v-if="on" class="text-sm text-green-600 dark:text-green-500" data-test="store-forward-on">
        Store and forward is on. Messages wait up to {{ days }} {{ days === 1 ? 'day' : 'days' }}.
      </p>
      <div v-if="on && held.length > 0" class="overflow-x-auto rounded-md border" data-test="store-forward-held">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Waiting</TableHead>
              <TableHead>From</TableHead>
              <TableHead>To</TableHead>
              <TableHead>Since</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="h in held" :key="h.id" :data-test="`held-${h.id}`">
              <TableCell class="whitespace-normal break-words">
                "{{ h.text }}"
                <p v-if="!h.format_known" class="text-xs text-muted-foreground" data-test="held-format-unknown">
                  Waits until your radio sends a text, so DMRHub can send it in your radio's format.
                </p>
              </TableCell>
              <TableCell>{{ h.source_callsign || h.source_id }}</TableCell>
              <TableCell class="font-mono tabular-nums">{{ h.radio_id }}</TableCell>
              <TableCell class="whitespace-nowrap">{{ shortDate(h.held_at) }}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
      <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { format } from 'date-fns';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { errorText, getHeldTexts, setStoreForward, type HeldText } from '@/services/settings';

const props = defineProps<{ enabled: boolean; holdDays?: number }>();

const on = ref(props.enabled);
const days = ref(props.holdDays && props.holdDays > 0 ? props.holdDays : 7);
const held = ref<HeldText[]>([]);
const saving = ref(false);
const error = ref('');

const shortDate = (value: string) => format(new Date(value), 'MMM d, HH:mm');

const loadHeld = async () => {
  try {
    held.value = await getHeldTexts();
  } catch (err) {
    error.value = errorText(err, 'Could not load the waiting messages.');
  }
};

const onSwitch = async (value: boolean) => {
  error.value = '';
  saving.value = true;
  try {
    const res = await setStoreForward(value);
    on.value = res.store_forward === true;
    if (res.hold_days && res.hold_days > 0) {
      days.value = res.hold_days;
    }
    held.value = [];
    if (on.value) {
      await loadHeld();
    }
  } catch (err) {
    error.value = errorText(err, value ? 'Could not turn on store and forward.' : 'Could not turn off store and forward.');
  } finally {
    saving.value = false;
  }
};

onMounted(() => {
  if (on.value) {
    loadHeld();
  }
});
</script>
