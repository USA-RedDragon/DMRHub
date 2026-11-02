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
  <Card data-test="on-air-card">
    <CardHeader>
      <CardTitle>On-air code</CardTitle>
      <CardDescription>
        Your radio IDs work on the air only after you unlock them with a code from an authenticator app. Nobody else
        can use your ID.
      </CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div v-if="view === 'off'">
        <ShadButton type="button" data-test="on-air-setup" :disabled="busy" @click="startSetup">Set up</ShadButton>
      </div>

      <div v-else-if="view === 'setup' && setup" class="grid items-start gap-x-6 gap-y-4 sm:grid-cols-[auto_minmax(0,1fr)]" data-test="on-air-steps">
        <div class="size-[168px] rounded-lg border border-border bg-white p-2.5">
          <img :src="setup.qr" alt="QR code for the authenticator app" class="size-full [image-rendering:pixelated]" data-test="on-air-qr" />
        </div>
        <div class="flex flex-col gap-3">
          <div>
            <b class="text-sm font-semibold">1. Scan the code with an authenticator app.</b>
            <div class="text-xs text-muted-foreground">
              Or enter this key: <span class="font-mono" data-test="on-air-key">{{ groupedKey }}</span>
            </div>
          </div>
          <form class="flex flex-col gap-1" novalidate @submit.prevent="turnOn">
            <label for="on-air-code" class="text-sm font-semibold">2. Enter the 6-digit code from the app</label>
            <div class="flex flex-wrap gap-2">
              <ShadInput
                id="on-air-code"
                v-model="code"
                inputmode="numeric"
                autocomplete="one-time-code"
                :maxlength="6"
                class="max-w-[140px] font-mono"
              />
              <ShadButton type="submit" data-test="on-air-turn-on" :disabled="busy">Turn on</ShadButton>
              <ShadButton variant="outline" type="button" @click="cancelSetup">Cancel</ShadButton>
            </div>
          </form>
        </div>
      </div>

      <template v-else-if="view === 'on' || view === 'off2'">
        <p class="text-sm" data-test="on-air-on">
          <b class="font-semibold">On.</b>
          <span class="text-muted-foreground">
            To unlock, make a private call to 9 and the 6-digit code that the app shows, such as 9482913 for 482913.
            Your radio stays unlocked on that hotspot or repeater timeslot for {{ lengthText }}.
          </span>
        </p>
        <Table v-if="unlocks.length > 0" data-test="on-air-unlocks">
          <TableHeader>
            <TableRow>
              <TableHead>Unlocked</TableHead>
              <TableHead>On</TableHead>
              <TableHead>Until</TableHead>
              <TableHead><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="u in unlocks" :key="`${u.radio}-${u.repeater}-${u.slot}`" :data-test="`unlock-${u.radio}-${u.repeater}-${u.slot}`">
              <TableCell class="font-mono">{{ u.radio }}</TableCell>
              <TableCell>{{ u.callsign ? `${u.callsign} ${u.repeater}` : u.repeater }}, TS{{ u.slot }}</TableCell>
              <TableCell class="whitespace-nowrap">{{ when(u.until) }}</TableCell>
              <TableCell>
                <ShadButton variant="outline" size="sm" type="button" data-test="lock" :disabled="busy" @click="lock(u)">Lock</ShadButton>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <p v-else class="text-sm text-muted-foreground" data-test="on-air-none">No radio is unlocked now.</p>
        <div v-if="view === 'on'">
          <ShadButton variant="outline" type="button" data-test="on-air-off" @click="askOff">Turn off</ShadButton>
        </div>
        <form
          v-else
          role="alert"
          class="flex flex-col gap-2 rounded-lg border border-yellow-500/50 bg-yellow-500/10 px-4 py-3 text-sm"
          data-test="on-air-confirm-off"
          novalidate
          @submit.prevent="turnOff"
        >
          <b class="font-semibold">Turn off the on-air code?</b>
          <p>Anyone can then transmit with your radio IDs.</p>
          <div class="flex flex-wrap gap-2">
            <ShadInput
              id="on-air-off-code"
              v-model="code"
              inputmode="numeric"
              autocomplete="one-time-code"
              :maxlength="6"
              placeholder="Code from the app"
              aria-label="Code from the app"
              class="max-w-[180px] font-mono"
            />
            <ShadButton variant="destructive" type="submit" data-test="on-air-turn-off" :disabled="busy">Turn off</ShadButton>
            <ShadButton variant="outline" type="button" @click="view = 'on'">Cancel</ShadButton>
          </div>
        </form>
      </template>

      <p v-if="error" class="text-sm text-destructive" data-test="on-air-error">{{ error }}</p>
      <p v-if="note" class="text-sm text-green-600 dark:text-green-500" data-test="on-air-note">{{ note }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { format } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { errorText } from '@/services/settings';
import {
  getOnAirCode,
  lockRadio,
  startOnAirSetup,
  turnOnAirCodeOff,
  turnOnAirCodeOn,
  type OnAirCode,
  type OnAirSetup,
  type OnAirUnlock,
} from '@/services/totp';

const view = ref<'loading' | 'off' | 'setup' | 'on' | 'off2'>('loading');
const setup = ref<OnAirSetup | null>(null);
const unlocks = ref<OnAirUnlock[]>([]);
const unlockSeconds = ref(24 * 3600);
const code = ref('');
const busy = ref(false);
const error = ref('');
const note = ref('');

const when = (value: string) => format(new Date(value), 'MMM d, HH:mm');

const groupedKey = computed(() => (setup.value?.key ?? '').replace(/(.{4})(?=.)/g, '$1 '));

const lengthText = computed(() => {
  const s = unlockSeconds.value;
  if (s % 86400 === 0) {
    return s === 86400 ? '24 hours' : `${s / 86400} days`;
  }
  if (s % 3600 === 0) {
    return s === 3600 ? '1 hour' : `${s / 3600} hours`;
  }
  const m = Math.max(1, Math.round(s / 60));
  return m === 1 ? '1 minute' : `${m} minutes`;
});

const show = (s: OnAirCode) => {
  unlocks.value = s.unlocks;
  if (s.unlock_seconds > 0) {
    unlockSeconds.value = s.unlock_seconds;
  }
  view.value = s.on ? 'on' : 'off';
};

const clear = () => {
  error.value = '';
  note.value = '';
  code.value = '';
};

const load = async () => {
  try {
    show(await getOnAirCode());
  } catch (err) {
    view.value = 'off';
    error.value = errorText(err, 'Could not load your on-air code.');
  }
};

const startSetup = async () => {
  clear();
  busy.value = true;
  try {
    setup.value = await startOnAirSetup();
    view.value = 'setup';
  } catch (err) {
    error.value = errorText(err, 'Could not start the setup.');
  } finally {
    busy.value = false;
  }
};

const cancelSetup = () => {
  clear();
  setup.value = null;
  view.value = 'off';
};

const validCode = () => {
  if (!/^\d{6}$/.test(code.value.replace(/\s/g, ''))) {
    error.value = 'Enter the 6-digit code from your app.';
    return false;
  }
  return true;
};

const turnOn = async () => {
  error.value = '';
  if (!validCode()) {
    return;
  }
  busy.value = true;
  try {
    show(await turnOnAirCodeOn(code.value.replace(/\s/g, '')));
    setup.value = null;
    code.value = '';
    note.value = 'The on-air code is on.';
  } catch (err) {
    error.value = errorText(err, 'Could not turn the on-air code on.');
  } finally {
    busy.value = false;
  }
};

const askOff = () => {
  clear();
  view.value = 'off2';
};

const turnOff = async () => {
  error.value = '';
  if (!validCode()) {
    return;
  }
  busy.value = true;
  try {
    show(await turnOnAirCodeOff(code.value.replace(/\s/g, '')));
    code.value = '';
    note.value = 'The on-air code is off.';
  } catch (err) {
    error.value = errorText(err, 'Could not turn the on-air code off.');
  } finally {
    busy.value = false;
  }
};

const lock = async (u: OnAirUnlock) => {
  clear();
  busy.value = true;
  try {
    show(await lockRadio(u));
    note.value = `Locked ${u.radio} on ${u.callsign || u.repeater}.`;
  } catch (err) {
    error.value = errorText(err, `Could not lock ${u.radio}.`);
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>
