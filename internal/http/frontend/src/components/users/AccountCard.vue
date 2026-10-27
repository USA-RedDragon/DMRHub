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
  <Card data-test="account-card">
    <CardHeader>
      <CardTitle>Account</CardTitle>
    </CardHeader>
    <form class="contents" novalidate @submit.prevent="save">
      <CardContent class="flex flex-col gap-4">
        <div class="grid grid-cols-[repeat(auto-fit,minmax(13rem,1fr))] gap-4">
          <div class="flex min-w-0 flex-col gap-1">
            <label for="account-dmr-id" class="text-sm">DMR ID</label>
            <ShadInput id="account-dmr-id" :model-value="String(id)" readonly class="bg-muted text-muted-foreground" />
          </div>
          <div class="flex min-w-0 flex-col gap-1">
            <label for="account-username" class="text-sm">Username</label>
            <ShadInput id="account-username" v-model="username" autocomplete="username" />
          </div>
          <div class="flex min-w-0 flex-col gap-1">
            <label for="account-callsign" class="text-sm">Callsign</label>
            <ShadInput
              id="account-callsign"
              v-model="callsign"
              autocapitalize="characters"
              :aria-invalid="error ? 'true' : undefined"
            />
            <p v-if="error" class="text-sm text-destructive" data-test="account-error">{{ error }}</p>
          </div>
        </div>
        <div
          v-if="showHint"
          class="flex flex-wrap items-center justify-between gap-2 rounded-[10px] border border-warn-border bg-warn-bg px-3.5 py-2.5 text-sm text-warn-fg"
          data-test="account-radioid"
        >
          <span>RadioID lists DMR ID {{ id }} as <b>{{ listed }}</b>.</span>
          <ShadButton variant="outline" size="sm" type="button" data-test="account-use-radioid" @click="useListed">
            Use {{ listed }}
          </ShadButton>
        </div>
      </CardContent>
      <CardFooter class="flex items-center justify-end gap-3">
        <p v-if="saved" class="mr-auto text-sm text-green-600 dark:text-green-500" data-test="account-ok">{{ saved }}</p>
        <ShadButton type="submit" :disabled="busy">Save</ShadButton>
      </CardFooter>
    </form>
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { patchAccount, radioIDCallsign } from '@/services/radioid';
import { errorText } from '@/services/settings';
import { useSettingsStore } from '@/store';

const props = defineProps<{ id: number; username: string; callsign: string }>();
const emit = defineEmits<{ saved: [username: string, callsign: string] }>();

const settingsStore = useSettingsStore();
const username = ref(props.username);
const callsign = ref(props.callsign);
const current = ref(props.callsign.toUpperCase());
const listed = ref('');
const error = ref('');
const saved = ref('');
const busy = ref(false);

const showHint = computed(
  () => settingsStore.radioIDValidation && listed.value !== '' && listed.value !== current.value,
);

onMounted(async () => {
  await settingsStore.fetchConfig();
  if (settingsStore.radioIDValidation) {
    listed.value = await radioIDCallsign(props.id);
  }
});

const useListed = () => {
  callsign.value = listed.value;
  error.value = '';
  saved.value = '';
};

const save = async () => {
  error.value = '';
  saved.value = '';
  const name = username.value.trim();
  const call = callsign.value.trim().toUpperCase();
  busy.value = true;
  try {
    await patchAccount(props.id, { username: name, callsign: call });
    username.value = name;
    callsign.value = call;
    current.value = call;
    saved.value = 'Saved';
    emit('saved', name, call);
  } catch (err) {
    error.value = errorText(err, 'Could not save your account.');
  } finally {
    busy.value = false;
  }
};
</script>
