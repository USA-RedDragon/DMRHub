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
  <Card data-test="cosysops">
    <CardHeader>
      <CardTitle>Co-sysops</CardTitle>
      <CardDescription v-if="manages">
        They can change this repeater's talkgroups, rules and codec, drop calls and see its connection log. An invite
        waits 7 days for its answer.
      </CardDescription>
      <CardDescription v-else-if="data.owner">Owner: {{ data.owner.callsign }}.</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div v-if="data.cosysops.length > 0" class="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Callsign</TableHead>
              <TableHead>Username</TableHead>
              <TableHead>Added</TableHead>
              <TableHead v-if="hasActions"><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="c in data.cosysops" :key="c.id" :data-test="`cosysop-${c.id}`">
              <TableCell>{{ c.callsign }}</TableCell>
              <TableCell>{{ c.username }}</TableCell>
              <TableCell>
                <span v-if="c.pending" class="text-muted-foreground">Invited, answer by {{ day(c.expires_at) }}</span>
                <span v-else>{{ day(c.accepted_at) }}</span>
              </TableCell>
              <TableCell v-if="hasActions" class="text-right">
                <ShadButton
                  v-if="manages || c.id === myId"
                  variant="outline"
                  size="sm"
                  :disabled="busy"
                  :data-test="`remove-${c.id}`"
                  @click="remove(c)"
                >{{ removeLabel(c) }}</ShadButton>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
      <p v-else-if="manages" class="text-sm text-muted-foreground">No co-sysops yet.</p>
      <form v-if="manages" class="flex flex-wrap items-center gap-2" novalidate @submit.prevent="invite">
        <ShadInput
          v-model="name"
          class="h-9 min-w-0 flex-[0_1_16rem]"
          placeholder="Callsign or username"
          aria-label="Callsign or username"
          :aria-invalid="failed"
          data-test="invite-user"
        />
        <ShadButton type="submit" variant="outline" :disabled="busy || !name.trim()" data-test="invite">Invite</ShadButton>
      </form>
      <p v-if="message" class="text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'" data-test="cosysops-message">
        {{ message }}
      </p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { format } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import API from '@/services/API';
import { errorText } from '@/services/settings';
import type { Cosysop, Cosysops } from '@/lib/cosysops';

const props = defineProps<{ repeaterId: number; data: Cosysops; myId: number }>();
const emit = defineEmits<{ changed: [] }>();

const name = ref('');
const busy = ref(false);
const message = ref('');
const failed = ref(false);

const manages = computed(() => props.data.role === 'owner' || props.data.role === 'admin');
const hasActions = computed(() => manages.value || props.data.cosysops.some((c) => c.id === props.myId));

const day = (iso: string | null) => (iso ? format(new Date(iso), 'MMM d, yyyy') : '');

const removeLabel = (c: Cosysop) => {
  if (c.id === props.myId) return 'Leave';
  return c.pending ? 'Cancel invite' : 'Remove';
};

const invite = async () => {
  const user = name.value.trim();
  if (!user) return;
  busy.value = true;
  failed.value = false;
  message.value = '';
  try {
    const res = await API.post(`/repeaters/${props.repeaterId}/cosysops`, { user });
    message.value = `Invited ${res.data.callsign}. They see the invite on their Repeaters page.`;
    name.value = '';
    emit('changed');
  } catch (err) {
    failed.value = true;
    message.value = errorText(err, 'Could not invite. Try again later.');
  } finally {
    busy.value = false;
  }
};

const remove = async (c: Cosysop) => {
  busy.value = true;
  failed.value = false;
  message.value = '';
  try {
    await API.delete(`/repeaters/${props.repeaterId}/cosysops/${c.id}`);
    emit('changed');
  } catch (err) {
    failed.value = true;
    message.value = errorText(err, 'Could not remove. Try again later.');
  } finally {
    busy.value = false;
  }
};
</script>
