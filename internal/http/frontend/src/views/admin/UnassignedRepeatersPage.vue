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
    <h1>Unassigned Repeaters</h1>
    <p v-if="message" class="text-sm text-green-600 dark:text-green-500" data-test="unassigned-ok">{{ message }}</p>
    <p v-if="error" class="text-sm text-destructive" data-test="unassigned-error">{{ error }}</p>
    <p v-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>
    <Card v-else-if="loaded" class="py-2">
      <p v-if="repeaters.length === 0" class="px-6 py-5 text-center text-muted-foreground" data-test="unassigned-empty">
        No unassigned repeaters
      </p>
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead class="hidden sm:table-cell">Kind</TableHead>
            <TableHead>Callsign</TableHead>
            <TableHead class="hidden md:table-cell">Frequencies</TableHead>
            <TableHead class="hidden md:table-cell">Color code</TableHead>
            <TableHead class="hidden sm:table-cell">Connected</TableHead>
            <TableHead class="hidden lg:table-cell">Made</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="r in repeaters" :key="r.id" data-test="unassigned-row">
            <TableCell class="font-mono tabular-nums">{{ r.id }}</TableCell>
            <TableCell class="hidden sm:table-cell">{{ kindLabel(r) }}</TableCell>
            <TableCell>
              <span v-if="r.callsign" class="font-mono">{{ r.callsign }}</span>
              <span v-else class="text-muted-foreground">None</span>
            </TableCell>
            <TableCell class="hidden font-mono tabular-nums md:table-cell">{{ frequencyLabel(r) }}</TableCell>
            <TableCell class="hidden font-mono tabular-nums md:table-cell">{{ r.color_code }}</TableCell>
            <TableCell class="hidden text-muted-foreground sm:table-cell">
              <RelativeTimestamp v-if="seen(r.connected_time)" :time="r.connected_time" />
              <span v-else>Never</span>
            </TableCell>
            <TableCell class="hidden text-muted-foreground lg:table-cell">{{ shortDate(r.created_at) }}</TableCell>
            <TableCell class="text-right">
              <ShadButton size="sm" type="button" data-test="unassigned-assign" @click="ask(r)">Assign owner</ShadButton>
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

    <Dialog :open="assigning !== null" @update:open="(open: boolean) => { if (!open) assigning = null; }">
      <DialogContent v-if="assigning" data-test="unassigned-dialog">
        <DialogTitle>Assign an owner to repeater {{ assigning.id }}</DialogTitle>
        <DialogDescription class="sr-only">Pick the owner</DialogDescription>
        <ShadInput
          v-model="query"
          type="search"
          placeholder="Callsign, username or DMR ID"
          aria-label="Find a user"
          data-test="unassigned-search"
        />
        <div class="max-h-56 overflow-y-auto rounded-md border border-border">
          <p v-if="matches.length === 0" class="px-3 py-2.5 text-sm text-muted-foreground" data-test="unassigned-nomatch">No user matches</p>
          <label
            v-for="u in matches"
            :key="u.id"
            class="flex cursor-pointer items-center gap-2.5 border-b border-border px-3 py-2.5 last:border-b-0 hover:bg-accent"
            data-test="unassigned-user"
          >
            <input v-model="picked" type="radio" name="owner" :value="u.id" class="size-4 accent-primary" />
            <span class="flex flex-col">
              <span><b class="font-mono">{{ u.callsign }}</b> {{ u.username }}</span>
              <span class="font-mono text-xs text-muted-foreground tabular-nums">DMR ID {{ u.id }}</span>
            </span>
          </label>
        </div>
        <div class="flex flex-wrap justify-end gap-2">
          <ShadButton variant="outline" type="button" @click="assigning = null">Cancel</ShadButton>
          <ShadButton type="button" :disabled="busy || picked === null" data-test="unassigned-confirm" @click="assign">
            Assign owner
          </ShadButton>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import { format } from 'date-fns';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import API from '@/services/API';
import {
  assignRepeaterOwner,
  frequencyLabel,
  getUnassignedRepeaters,
  kindLabel,
  type UnassignedRepeater,
} from '@/services/repeater';
import { errorText } from '@/services/settings';
import { useAdminStore } from '@/store';

useHead({ title: 'Unassigned Repeaters' });

type Owner = { id: number; callsign: string; username: string };

const adminStore = useAdminStore();
const pageSize = 100;
const repeaters = ref<UnassignedRepeater[]>([]);
const total = ref(0);
const page = ref(1);
const loaded = ref(false);
const loadError = ref('');
const users = ref<Owner[]>([]);
const assigning = ref<UnassignedRepeater | null>(null);
const query = ref('');
const picked = ref<number | null>(null);
const busy = ref(false);
const message = ref('');
const error = ref('');

const shortDate = (value: string) => format(new Date(value), 'MMM d, yyyy');
const seen = (value: string) => !!value && new Date(value).getUTCFullYear() > 1;

const matches = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) {
    return users.value;
  }
  return users.value.filter((u) => u.callsign.toLowerCase().includes(q) || u.username.toLowerCase().includes(q) || String(u.id).includes(q));
});

const load = async (to = 1) => {
  try {
    const list = await getUnassignedRepeaters(to, pageSize);
    repeaters.value = list.repeaters;
    total.value = list.total;
    page.value = to;
    adminStore.unassignedRepeaters = list.total;
    loadError.value = '';
  } catch (err) {
    loadError.value = errorText(err, 'Could not load the unassigned repeaters.');
  } finally {
    loaded.value = true;
  }
};

const loadUsers = async () => {
  try {
    const res = await API.get('/users?limit=none');
    const list = (res.data?.users ?? []) as { id: number; callsign: string; username: string; approved: boolean; suspended: boolean }[];
    users.value = list.filter((u) => u.approved && !u.suspended).map((u) => ({ id: u.id, callsign: u.callsign, username: u.username }));
  } catch (err) {
    error.value = errorText(err, 'Could not load the users.');
  }
};

const ask = (r: UnassignedRepeater) => {
  message.value = '';
  error.value = '';
  query.value = '';
  picked.value = null;
  assigning.value = r;
  if (users.value.length === 0) {
    void loadUsers();
  }
};

const assign = async () => {
  const r = assigning.value;
  const owner = users.value.find((u) => u.id === picked.value);
  if (!r || !owner) {
    return;
  }
  busy.value = true;
  try {
    await assignRepeaterOwner(r.id, owner.id);
    message.value = `${owner.callsign} now owns repeater ${r.id}.`;
  } catch (err) {
    error.value = errorText(err, `Could not assign the owner of ${r.id}.`);
  } finally {
    assigning.value = null;
    busy.value = false;
    await load(page.value);
  }
};

onMounted(() => load());
</script>
