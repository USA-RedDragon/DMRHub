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
    <div class="flex flex-wrap items-end justify-between gap-2">
      <h1>Invites</h1>
      <ShadButton type="button" :disabled="busy" data-test="invite-new" @click="make">New invite</ShadButton>
    </div>
    <p v-if="error" class="text-sm text-destructive" data-test="invite-error">{{ error }}</p>
    <div
      v-if="made"
      class="grid gap-2 rounded-lg border border-primary bg-primary/5 px-3.5 py-3"
      data-test="invite-once"
    >
      <b>Invite link</b>
      <div class="flex flex-wrap items-center gap-2">
        <ShadInput :model-value="made.link" readonly class="min-w-[220px] flex-1 font-mono" aria-label="Invite link" />
        <CopyButton :text="made.link" />
      </div>
      <span class="text-xs text-muted-foreground">Shown once. Works once, for 7 days.</span>
    </div>
    <p v-if="loadError" class="text-sm text-destructive">{{ loadError }}</p>
    <Card v-else-if="loaded" class="py-2">
      <p v-if="invites.length === 0" class="px-6 py-5 text-center text-muted-foreground" data-test="invite-empty">No invites</p>
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>Made</TableHead>
            <TableHead>By</TableHead>
            <TableHead>Expires</TableHead>
            <TableHead>Used by</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="i in invites" :key="i.id" data-test="invite-row">
            <TableCell>{{ when(i.created_at) }}</TableCell>
            <TableCell class="font-mono">{{ i.created_by_callsign }}</TableCell>
            <TableCell>
              <span v-if="i.used_at" class="text-muted-foreground">Used</span>
              <span v-else-if="expired(i)" class="text-muted-foreground">Expired</span>
              <span v-else>{{ when(i.expires_at) }}</span>
            </TableCell>
            <TableCell>
              <template v-if="i.used_at">
                <span class="font-mono">{{ i.used_by_callsign || i.used_by }}</span>
                <span class="ml-1.5 text-xs text-muted-foreground">{{ when(i.used_at) }}</span>
              </template>
            </TableCell>
            <TableCell class="text-right">
              <ShadButton
                v-if="!i.used_at"
                variant="outline"
                size="sm"
                type="button"
                :disabled="busy"
                data-test="invite-delete"
                @click="remove(i)"
              >
                Delete
              </ShadButton>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import { format } from 'date-fns';
import CopyButton from '@/components/enroll/CopyButton.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { createInvite, deleteInvite, getInvites, type Invite, type NewInvite } from '@/services/accounts';
import { errorText } from '@/services/settings';

useHead({ title: 'Invites' });

const invites = ref<Invite[]>([]);
const loaded = ref(false);
const loadError = ref('');
const error = ref('');
const busy = ref(false);
const made = ref<NewInvite | null>(null);

const when = (value: string) => format(new Date(value), 'MMM d, HH:mm');
const expired = (i: Invite) => new Date(i.expires_at).getTime() <= Date.now();

const load = async () => {
  try {
    invites.value = await getInvites();
    loadError.value = '';
  } catch (err) {
    loadError.value = errorText(err, 'Could not load the invites.');
  } finally {
    loaded.value = true;
  }
};

const make = async () => {
  busy.value = true;
  error.value = '';
  try {
    made.value = await createInvite();
    await load();
  } catch (err) {
    error.value = errorText(err, 'Could not make an invite.');
  } finally {
    busy.value = false;
  }
};

const remove = async (i: Invite) => {
  busy.value = true;
  error.value = '';
  try {
    await deleteInvite(i.id);
    await load();
  } catch (err) {
    error.value = errorText(err, 'Could not delete the invite.');
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>
