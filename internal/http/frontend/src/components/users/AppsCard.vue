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
  <Card data-test="apps-card">
    <CardHeader>
      <CardTitle>Apps</CardTitle>
      <CardDescription>
        Web apps that you allowed to use your DMRHub account. Removing one signs it out at once.
      </CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <Table v-if="apps.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>App</TableHead>
            <TableHead>Can use</TableHead>
            <TableHead>Allowed</TableHead>
            <TableHead>Last used</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="app in apps" :key="app.id" :data-test="`app-${app.id}`">
            <TableCell class="font-mono font-semibold break-all">{{ app.origin }}</TableCell>
            <TableCell>{{ app.can_use }}</TableCell>
            <TableCell>{{ day(app.allowed) }}</TableCell>
            <TableCell>{{ ago(app.last_used) }}</TableCell>
            <TableCell>
              <ShadButton variant="outline" size="sm" type="button" :disabled="busy" @click="remove(app)">
                Remove
              </ShadButton>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
      <p v-else-if="loaded" class="text-sm text-muted-foreground">No apps can use your account.</p>
      <p v-if="removed" class="text-sm text-green-600 dark:text-green-500">Removed {{ removed }}. It is signed out.</p>
      <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { format, formatDistanceToNow } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { getApps, removeApp, type App } from '@/services/oauth';
import { errorText } from '@/services/settings';

const apps = ref<App[]>([]);
const loaded = ref(false);
const busy = ref(false);
const removed = ref('');
const error = ref('');

const day = (value: string) => format(new Date(value), 'MMM d, yyyy');
const ago = (value: string) => formatDistanceToNow(new Date(value), { addSuffix: true });

const load = async () => {
  try {
    apps.value = await getApps();
  } catch (err) {
    error.value = errorText(err, 'Could not load your apps.');
  } finally {
    loaded.value = true;
  }
};

const remove = async (app: App) => {
  busy.value = true;
  error.value = '';
  removed.value = '';
  try {
    await removeApp(app.id);
    apps.value = apps.value.filter((a) => a.id !== app.id);
    removed.value = app.origin;
  } catch (err) {
    error.value = errorText(err, `Could not remove ${app.origin}.`);
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>
