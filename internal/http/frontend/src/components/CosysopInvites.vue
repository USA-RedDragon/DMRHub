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
  <Card v-if="invites.length > 0" class="mb-6" data-test="cosysop-invites">
    <CardHeader>
      <CardTitle>Co-sysop invites</CardTitle>
      <CardDescription>
        As a co-sysop you can change the talkgroups, rules and codec of a repeater, drop calls and see its connection
        log.
      </CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <div
        v-for="inv in invites"
        :key="inv.repeater_id"
        class="flex flex-wrap items-center gap-3"
        :data-test="`invite-${inv.repeater_id}`"
      >
        <span class="mr-auto text-sm">
          <strong>{{ inv.owner ? inv.owner.callsign : 'The owner' }}</strong>
          invited you to manage
          <strong>{{ inv.repeater_callsign || 'repeater' }} {{ inv.repeater_id }}</strong>.
          <span class="text-muted-foreground">Answer by {{ day(inv.expires_at) }}.</span>
        </span>
        <ShadButton size="sm" :disabled="busy" :data-test="`accept-${inv.repeater_id}`" @click="answer(inv, true)">Accept</ShadButton>
        <ShadButton size="sm" variant="outline" :disabled="busy" :data-test="`decline-${inv.repeater_id}`" @click="answer(inv, false)">
          Decline
        </ShadButton>
      </div>
      <p v-if="message" class="text-sm text-destructive">{{ message }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { format } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import API from '@/services/API';
import { errorText } from '@/services/settings';
import type { CosysopInvite } from '@/lib/cosysops';

const emit = defineEmits<{ accepted: [] }>();

const invites = ref<CosysopInvite[]>([]);
const busy = ref(false);
const message = ref('');

const day = (iso: string) => format(new Date(iso), 'MMM d, yyyy');

const load = async () => {
  try {
    const res = await API.get('/repeaters/cosysop-invites');
    invites.value = Array.isArray(res.data?.invites) ? res.data.invites : [];
  } catch (err) {
    console.error(err);
  }
};

const answer = async (inv: CosysopInvite, accept: boolean) => {
  busy.value = true;
  message.value = '';
  try {
    await API.post(`/repeaters/${inv.repeater_id}/cosysop-invite/${accept ? 'accept' : 'decline'}`);
    invites.value = invites.value.filter((i) => i.repeater_id !== inv.repeater_id);
    if (accept) emit('accepted');
  } catch (err) {
    message.value = errorText(err, 'Could not answer the invite. Try again later.');
    await load();
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>
