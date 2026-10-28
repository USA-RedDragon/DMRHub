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
    <h1>Message access</h1>
    <div class="flex flex-wrap items-end gap-3">
      <div class="grid min-w-[180px] gap-1.5">
        <label for="access-radio-id" class="font-semibold">Radio ID</label>
        <ShadInput id="access-radio-id" v-model="radioID" inputmode="numeric" data-test="access-radio-id" @change="load(1)" />
      </div>
      <div class="grid min-w-[180px] gap-1.5">
        <label for="access-admin" class="font-semibold">Admin</label>
        <select
          id="access-admin"
          v-model="admin"
          class="h-9 rounded-md border border-input bg-background px-3"
          data-test="access-admin"
          @change="load(1)"
        >
          <option value="">All</option>
          <option v-for="a in admins" :key="a.id" :value="String(a.id)">{{ a.callsign }}</option>
        </select>
      </div>
    </div>
    <p v-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>
    <Card v-else-if="loaded" class="py-2">
      <p v-if="accesses.length === 0" class="px-6 py-5 text-center text-muted-foreground" data-test="access-empty">None</p>
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>Time</TableHead>
            <TableHead>Admin</TableHead>
            <TableHead>What</TableHead>
            <TableHead>Messages</TableHead>
            <TableHead>Radio IDs</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="a in accesses" :key="a.id" data-test="access-row">
            <TableCell class="whitespace-nowrap">{{ when(a.at) }}</TableCell>
            <TableCell class="font-mono">{{ a.admin_callsign || a.admin_id }}</TableCell>
            <TableCell>{{ accessWhat(a) }}</TableCell>
            <TableCell class="tabular-nums">{{ a.results }}</TableCell>
            <TableCell class="whitespace-normal">
              <span
                v-for="r in a.radio_ids"
                :key="r.radio_id"
                class="mr-1 mb-1 inline-flex items-center gap-1 rounded-full border px-2 text-xs leading-5"
                data-test="access-radio"
              ><b class="font-semibold tabular-nums">{{ r.radio_id }}</b><span v-if="r.callsign">{{ r.callsign }}</span></span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>
    <div v-if="loaded && total > 0" class="flex items-center justify-end gap-2 text-sm text-muted-foreground">
      <span data-test="access-count">{{ accesses.length }} of {{ total }}</span>
      <ShadButton variant="outline" size="sm" type="button" :disabled="page <= 1" @click="load(page - 1)">Previous</ShadButton>
      <ShadButton variant="outline" size="sm" type="button" :disabled="page * pageSize >= total" @click="load(page + 1)">Next</ShadButton>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import { format } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import API from '@/services/API';
import { accessWhat, getMessageAccesses, type MessageAccess } from '@/services/accounts';
import { errorText } from '@/services/settings';

useHead({ title: 'Message access' });

const pageSize = 50;
const accesses = ref<MessageAccess[]>([]);
const admins = ref<{ id: number; callsign: string }[]>([]);
const total = ref(0);
const page = ref(1);
const loaded = ref(false);
const loadError = ref('');
const radioID = ref('');
const admin = ref('');

const when = (value: string) => format(new Date(value), 'MMM d, HH:mm');

const load = async (to = 1) => {
  try {
    const rid = Number(radioID.value.trim());
    const list = await getMessageAccesses({
      page: to,
      limit: pageSize,
      radioID: Number.isInteger(rid) && rid > 0 ? rid : undefined,
      admin: admin.value ? Number(admin.value) : undefined,
    });
    accesses.value = list.accesses;
    total.value = list.total;
    page.value = to;
    loadError.value = '';
  } catch (err) {
    loadError.value = errorText(err, 'Could not load the message access log.');
  } finally {
    loaded.value = true;
  }
};

const loadAdmins = async () => {
  try {
    const res = await API.get('/users?limit=none');
    const users = (res.data?.users ?? []) as { id: number; callsign: string; admin: boolean; superAdmin: boolean }[];
    admins.value = users.filter((u) => u.admin || u.superAdmin).map((u) => ({ id: u.id, callsign: u.callsign }));
  } catch {
    admins.value = [];
  }
};

onMounted(() => {
  void loadAdmins();
  void load();
});
</script>
