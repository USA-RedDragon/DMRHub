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
  <div class="overflow-x-auto rounded-md border" data-test="local-groups">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Talkgroup</TableHead>
          <TableHead>Repeaters</TableHead>
          <TableHead>Owner</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-if="loading">
          <TableCell colspan="4" class="text-muted-foreground">Loading...</TableCell>
        </TableRow>
        <TableRow v-else-if="error">
          <TableCell colspan="4" class="text-destructive">{{ error }}</TableCell>
        </TableRow>
        <TableRow v-else-if="groups.length === 0">
          <TableCell colspan="4" class="text-muted-foreground">No local groups yet</TableCell>
        </TableRow>
        <TableRow v-for="g in groups" v-else :key="g.id">
          <TableCell>
            <RouterLink :to="`/localgroups/${g.id}`" class="font-medium text-primary underline">{{ g.name }}</RouterLink>
          </TableCell>
          <TableCell><span class="inline-flex items-center rounded-full border bg-muted px-2.5 text-[0.8125rem] leading-[1.625rem]">TG {{ g.talkgroup }}</span></TableCell>
          <TableCell>{{ g.members }}</TableCell>
          <TableCell>{{ g.mine ? 'You' : g.owner.callsign }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { listLocalGroups, type LocalGroup } from '@/services/localGroups';
import { errorText } from '@/services/settings';

const groups = ref<LocalGroup[]>([]);
const loading = ref(true);
const error = ref('');

onMounted(async () => {
  try {
    groups.value = (await listLocalGroups()).groups ?? [];
  } catch (err) {
    error.value = errorText(err, 'Could not load the local groups');
  } finally {
    loading.value = false;
  }
});
</script>
