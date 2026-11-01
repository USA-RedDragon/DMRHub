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
  <Card v-if="alert && alert.smtp" data-test="offline-alert">
    <CardHeader>
      <CardTitle>Offline alert</CardTitle>
      <CardDescription>Get an email when this {{ device }} stops talking to DMRHub.</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div class="flex items-center justify-between gap-4">
        <label id="alert-on-label" for="alert-on" class="text-sm font-semibold">Email me when it is offline</label>
        <Switch
          id="alert-on"
          aria-labelledby="alert-on-label"
          :model-value="alert.enabled"
          :disabled="saving || !alert.email_verified"
          data-test="alert-on"
          @update:model-value="(v: boolean) => save({ enabled: v })"
        />
      </div>
      <p v-if="!alert.email_verified" class="text-sm text-muted-foreground" data-test="alert-no-email">
        Add an email address in <RouterLink to="/settings" class="text-primary underline">Settings</RouterLink> and
        verify it to get these emails.
      </p>
      <template v-if="alert.enabled">
        <div class="flex flex-col gap-1">
          <label for="alert-delay" class="text-sm">After</label>
          <select
            id="alert-delay"
            class="h-9 w-48 rounded-md border bg-transparent px-3 text-sm"
            :value="alert.delay_minutes"
            :disabled="saving"
            data-test="alert-delay"
            @change="(e) => save({ delay_minutes: Number((e.target as HTMLSelectElement).value) })"
          >
            <option v-for="d in alert.delays" :key="d" :value="d">{{ d }} minutes</option>
          </select>
        </div>
        <label class="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            class="size-4 accent-primary"
            :checked="alert.back"
            :disabled="saving"
            data-test="alert-back"
            @change="(e) => save({ back: (e.target as HTMLInputElement).checked })"
          />
          Also email me when it is back
        </label>
        <p class="text-xs text-muted-foreground" data-test="alert-email">Emails go to {{ alert.email }}.</p>
      </template>
      <p v-if="message" class="text-sm text-destructive" data-test="alert-message">{{ message }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import API from '@/services/API';
import { errorText } from '@/services/settings';

type Alert = {
  smtp: boolean;
  email_verified: boolean;
  email: string;
  enabled: boolean;
  delay_minutes: number;
  back: boolean;
  delays: number[];
};

const props = defineProps<{ repeaterId: number; device: string }>();

const alert = ref<Alert | null>(null);
const saving = ref(false);
const message = ref('');

onMounted(async () => {
  try {
    const res = await API.get(`/repeaters/${props.repeaterId}/alert`);
    alert.value = res.data as Alert;
  } catch (err) {
    console.error(err);
  }
});

const save = async (change: Partial<Pick<Alert, 'enabled' | 'delay_minutes' | 'back'>>) => {
  if (!alert.value) return;
  const body = {
    enabled: alert.value.enabled,
    delay_minutes: alert.value.delay_minutes,
    back: alert.value.back,
    ...change,
  };
  saving.value = true;
  message.value = '';
  try {
    const res = await API.put(`/repeaters/${props.repeaterId}/alert`, body);
    alert.value = res.data as Alert;
  } catch (err) {
    message.value = errorText(err, 'Could not save. Try again later.');
  } finally {
    saving.value = false;
  }
};
</script>
