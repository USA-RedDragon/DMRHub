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
  <Card data-test="talkgroup-listen">
    <CardHeader>
      <CardTitle>Listen-only monitor</CardTitle>
      <CardDescription>Visitors with no account can hear this talkgroup in DMRHub Radio. They cannot talk on it.</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-2">
      <div class="flex items-center justify-between gap-4">
        <label id="tg-listen-label" for="tg-listen" class="text-sm font-semibold">Let visitors listen</label>
        <Switch
          id="tg-listen"
          aria-labelledby="tg-listen-label"
          :model-value="listen"
          :disabled="saving"
          data-test="tg-listen"
          @update:model-value="save"
        />
      </div>
      <p v-if="message" class="text-sm text-destructive" data-test="tg-listen-message">{{ message }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import API from '@/services/API';
import { errorText } from '@/services/settings';

const props = defineProps<{ talkgroupId: number; noListen: boolean }>();

const listen = ref(!props.noListen);
const saving = ref(false);
const message = ref('');

watch(
  () => props.noListen,
  (v) => {
    listen.value = !v;
  },
);

const save = async (on: boolean) => {
  saving.value = true;
  message.value = '';
  try {
    await API.patch(`/talkgroups/${props.talkgroupId}`, { listen: on });
    listen.value = on;
  } catch (err) {
    message.value = errorText(err, 'Could not save. Try again later.');
  } finally {
    saving.value = false;
  }
};
</script>
