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
  <div class="flex flex-col gap-4">
    <h1>Settings</h1>

    <Card v-if="state === 'anon'" data-test="settings-anon">
      <CardHeader>
        <CardTitle>Log in to change your settings</CardTitle>
      </CardHeader>
    </Card>

    <template v-else-if="state === 'ready'">
      <Card data-test="sms-storage-card">
        <CardHeader>
          <CardTitle>SMS Storage</CardTitle>
          <CardDescription>Show the text messages sent to and from your radio IDs on the Messages page.</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex items-start justify-between gap-4">
            <div class="flex flex-col gap-1">
              <span id="sms-storage-label" class="font-medium">SMS Storage</span>
              <p class="text-sm" :class="smsStorage ? '' : 'text-muted-foreground'" data-test="sms-status">
                {{ smsStorage ? 'On.' : 'Off.' }}
              </p>
            </div>
            <Switch
              id="sms-storage"
              aria-labelledby="sms-storage-label"
              :model-value="smsStorage"
              :disabled="saving"
              @update:model-value="onSwitch"
            />
          </div>
          <div
            v-if="confirmOff"
            role="alert"
            class="flex flex-col gap-2 rounded-lg border border-yellow-500/50 bg-yellow-500/10 px-4 py-3 text-sm"
            data-test="sms-confirm-off"
          >
            <b class="font-semibold">Turn off SMS Storage?</b>
            <p>Your inbox and outbox will be empty.</p>
            <div class="flex flex-wrap gap-2">
              <ShadButton variant="destructive" size="sm" type="button" :disabled="saving" @click="turnOff">
                Turn off
              </ShadButton>
              <ShadButton variant="outline" size="sm" type="button" @click="confirmOff = false">Cancel</ShadButton>
            </div>
          </div>
          <p v-if="justEnabled" class="text-sm text-green-600 dark:text-green-500">SMS Storage is on.</p>
          <p v-if="smsError" class="text-sm text-destructive">{{ smsError }}</p>
          <p class="max-w-[70ch] border-l-[3px] border-border py-1 pl-3.5 text-sm" data-test="sms-privacy">
            {{ privacyText }}
          </p>
        </CardContent>
      </Card>

      <Card data-test="radio-ids-card">
        <CardHeader>
          <CardTitle>Radio IDs</CardTitle>
          <CardDescription>
            The radio IDs you own. Your messages page shows messages to and from these IDs. Each added ID must be
            registered to your callsign on RadioID.net.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Radio ID</TableHead>
                <TableHead>Callsign</TableHead>
                <TableHead>Status</TableHead>
                <TableHead><span class="sr-only">Actions</span></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="rid in radioIDs" :key="rid.id" :data-test="`radio-id-${rid.id}`">
                <TableCell class="font-bold tabular-nums">{{ rid.id }}</TableCell>
                <TableCell>{{ rid.callsign }}</TableCell>
                <TableCell>
                  <span v-if="rid.account" :class="badgeClass('info')">Account ID</span>
                  <template v-else>
                    <span :class="badgeClass('ok')">Verified</span>
                    <span v-if="rid.verified_at" class="ml-1 text-xs text-muted-foreground">
                      {{ shortDate(rid.verified_at) }}
                    </span>
                  </template>
                </TableCell>
                <TableCell>
                  <span v-if="rid.account" class="text-xs text-muted-foreground">Cannot remove</span>
                  <ShadButton
                    v-else
                    variant="outline"
                    size="sm"
                    type="button"
                    :disabled="saving"
                    @click="remove(rid.id)"
                  >
                    Remove
                  </ShadButton>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          <form class="flex flex-wrap items-end gap-2" data-test="add-radio-id" @submit.prevent="add">
            <div class="flex min-w-0 flex-[1_1_12rem] flex-col gap-1">
              <label for="add-radio-id" class="text-sm">Add a radio ID</label>
              <ShadInput
                id="add-radio-id"
                v-model="newID"
                inputmode="numeric"
                autocomplete="off"
                placeholder="7-digit DMR ID, for example 3191877"
              />
            </div>
            <ShadButton type="submit" :disabled="saving">Add</ShadButton>
          </form>
          <p v-if="idError" class="text-sm text-destructive" data-test="radio-id-error">{{ idError }}</p>
          <p v-if="idOK" class="text-sm text-green-600 dark:text-green-500" data-test="radio-id-ok">{{ idOK }}</p>
        </CardContent>
      </Card>
    </template>

    <Card v-else-if="state === 'error'">
      <CardHeader>
        <CardTitle>Error</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-destructive">Could not load your settings.</p>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import { format } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { badgeClass } from '@/lib/badge';
import {
  addRadioID,
  errorStatus,
  errorText,
  getRadioIDs,
  getSettings,
  removeRadioID,
  setSMSStorage,
  type RadioID,
} from '@/services/settings';

useHead({ title: 'Settings' });

const privacyText =
  'When SMS Storage is on, the text messages sent to and from the radio IDs that you own appear on your ' +
  'Messages page, up to the 250 newest in each box.';

const state = ref<'loading' | 'anon' | 'ready' | 'error'>('loading');
const smsStorage = ref(false);
const confirmOff = ref(false);
const justEnabled = ref(false);
const smsError = ref('');
const saving = ref(false);
const radioIDs = ref<RadioID[]>([]);
const newID = ref('');
const idError = ref('');
const idOK = ref('');

const shortDate = (value: string) => format(new Date(value), 'MMM d, h:mm a');

const load = async () => {
  try {
    const [settings, ids] = await Promise.all([getSettings(), getRadioIDs()]);
    smsStorage.value = settings.sms_storage;
    radioIDs.value = ids;
    state.value = 'ready';
  } catch (err) {
    state.value = errorStatus(err) === 401 ? 'anon' : 'error';
  }
};

const onSwitch = async (value: boolean) => {
  smsError.value = '';
  if (!value) {
    if (smsStorage.value) {
      confirmOff.value = true;
      justEnabled.value = false;
    }
    return;
  }
  saving.value = true;
  try {
    const res = await setSMSStorage(true);
    smsStorage.value = res.sms_storage;
    confirmOff.value = false;
    justEnabled.value = res.sms_storage;
  } catch (err) {
    smsError.value = errorText(err, 'Could not turn on SMS Storage.');
  } finally {
    saving.value = false;
  }
};

const turnOff = async () => {
  smsError.value = '';
  saving.value = true;
  try {
    const res = await setSMSStorage(false);
    smsStorage.value = res.sms_storage;
    confirmOff.value = false;
    justEnabled.value = false;
  } catch (err) {
    smsError.value = errorText(err, 'Could not turn off SMS Storage.');
  } finally {
    saving.value = false;
  }
};

const add = async () => {
  idError.value = '';
  idOK.value = '';
  const value = newID.value.trim();
  if (!/^\d{7}$/.test(value)) {
    idError.value = 'Enter a 7-digit DMR ID. Hotspot and repeater IDs go on the Repeaters page.';
    return;
  }
  const id = Number(value);
  if (radioIDs.value.some((rid) => rid.id === id)) {
    idError.value = `${id} is already on your list.`;
    return;
  }
  saving.value = true;
  try {
    const rid = await addRadioID(id);
    radioIDs.value = [...radioIDs.value, rid];
    newID.value = '';
    idOK.value = `Added ${id}.`;
  } catch (err) {
    idError.value = errorText(err, `Could not add ${id}.`);
  } finally {
    saving.value = false;
  }
};

const remove = async (id: number) => {
  idError.value = '';
  idOK.value = '';
  saving.value = true;
  try {
    await removeRadioID(id);
    radioIDs.value = radioIDs.value.filter((rid) => rid.id !== id);
    idOK.value = `Removed ${id}.`;
  } catch (err) {
    idError.value = errorText(err, `Could not remove ${id}.`);
  } finally {
    saving.value = false;
  }
};

onMounted(load);
</script>
