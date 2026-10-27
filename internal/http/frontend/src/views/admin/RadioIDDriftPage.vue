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
    <h1>RadioID Drift</h1>
    <p v-if="message" class="text-sm text-green-600 dark:text-green-500" data-test="drift-ok">{{ message }}</p>
    <p v-if="error" class="text-sm text-destructive" data-test="drift-error">{{ error }}</p>
    <p v-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>
    <Card v-else-if="loaded" class="py-2">
      <p v-if="drifts.length === 0" class="px-6 py-5 text-center text-muted-foreground" data-test="drift-empty">
        Every callsign matches RadioID.
      </p>
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>User</TableHead>
            <TableHead class="hidden sm:table-cell">DMR ID</TableHead>
            <TableHead>Callsign</TableHead>
            <TableHead>RadioID</TableHead>
            <TableHead class="hidden sm:table-cell">Found</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="d in drifts" :key="d.radio_id" data-test="drift-row">
            <TableCell>{{ d.username }}</TableCell>
            <TableCell class="hidden font-mono tabular-nums sm:table-cell">{{ d.radio_id }}</TableCell>
            <TableCell class="font-mono">{{ d.callsign }}</TableCell>
            <TableCell>
              <span v-if="d.radio_id_callsign" class="font-mono">{{ d.radio_id_callsign }}</span>
              <span v-else :class="badgeClass('none')">Not listed</span>
            </TableCell>
            <TableCell class="hidden text-muted-foreground sm:table-cell">{{ shortDate(d.found_at) }}</TableCell>
            <TableCell class="text-right">
              <ShadButton v-if="d.can_fix" variant="outline" size="sm" type="button" data-test="drift-fix" @click="ask(d)">
                Fix
              </ShadButton>
              <router-link v-else to="/admin/users" class="text-sm" data-test="drift-edit">Edit user</router-link>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>
    <div v-if="total > pageSize" class="flex justify-end gap-2">
      <ShadButton variant="outline" size="sm" type="button" :disabled="page <= 1" @click="load(page - 1)">
        Previous
      </ShadButton>
      <ShadButton variant="outline" size="sm" type="button" :disabled="page * pageSize >= total" @click="load(page + 1)">
        Next
      </ShadButton>
    </div>

    <Dialog :open="fixing !== null" @update:open="(open: boolean) => { if (!open) fixing = null; }">
      <DialogContent v-if="fixing" data-test="drift-dialog">
        <DialogTitle>Change the callsign of {{ fixing.username }} to {{ fixing.radio_id_callsign }}?</DialogTitle>
        <DialogDescription class="sr-only">Change the callsign</DialogDescription>
        <label v-if="fixing.can_notify" class="flex items-center gap-2 text-sm">
          <input v-model="notify" type="checkbox" class="size-4 accent-primary" data-test="drift-notify" />
          Email the user
        </label>
        <div class="flex flex-wrap justify-end gap-2">
          <ShadButton variant="outline" type="button" @click="fixing = null">Cancel</ShadButton>
          <ShadButton type="button" :disabled="busy" data-test="drift-confirm" @click="fix">Change callsign</ShadButton>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import { format } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { badgeClass } from '@/lib/badge';
import { fixRadioIDDrift, getRadioIDDrifts, type RadioIDDrift } from '@/services/radioid';
import { errorText } from '@/services/settings';

useHead({ title: 'RadioID Drift' });

const pageSize = 100;
const drifts = ref<RadioIDDrift[]>([]);
const total = ref(0);
const page = ref(1);
const loaded = ref(false);
const loadError = ref('');
const fixing = ref<RadioIDDrift | null>(null);
const notify = ref(true);
const busy = ref(false);
const message = ref('');
const error = ref('');

const shortDate = (value: string) => format(new Date(value), 'MMM d');

const load = async (to = 1) => {
  try {
    const list = await getRadioIDDrifts(to, pageSize);
    drifts.value = list.drifts;
    total.value = list.total;
    page.value = to;
    loadError.value = '';
  } catch (err) {
    loadError.value = errorText(err, 'Could not load the RadioID drift.');
  } finally {
    loaded.value = true;
  }
};

const ask = (d: RadioIDDrift) => {
  message.value = '';
  error.value = '';
  notify.value = true;
  fixing.value = d;
};

const fix = async () => {
  const d = fixing.value;
  if (!d) {
    return;
  }
  busy.value = true;
  try {
    const res = await fixRadioIDDrift(d.radio_id, d.can_notify && notify.value);
    message.value = `${d.username} is now ${res.callsign}.` + (res.notified ? ' Email sent.' : '');
    fixing.value = null;
    await load(page.value);
  } catch (err) {
    error.value = errorText(err, `Could not fix ${d.radio_id}.`);
    fixing.value = null;
  } finally {
    busy.value = false;
  }
};

onMounted(() => load());
</script>
