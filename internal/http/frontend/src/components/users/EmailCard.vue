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
  <Card data-test="email-card">
    <CardHeader>
      <CardTitle>Email</CardTitle>
      <CardDescription v-if="canEmail">
        DMRHub sends password reset links only to a confirmed email address.
      </CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <p v-if="!canEmail" class="text-sm" data-test="email-off">
        This network cannot send email. To reset a forgotten password, ask an admin.
      </p>
      <template v-else>
        <p v-if="saved" class="text-sm" data-test="email-status">
          {{ saved }}
          <span :class="badgeClass(verified ? 'ok' : 'warn')" class="ml-1">{{ verified ? 'Confirmed' : 'Not confirmed' }}</span>
        </p>
        <p v-else class="text-sm text-muted-foreground" data-test="email-status">No email address.</p>
        <form class="flex flex-wrap items-end gap-2" novalidate @submit.prevent="save">
          <div class="flex min-w-0 flex-[1_1_16rem] flex-col gap-1">
            <label for="email" class="text-sm">Email address</label>
            <ShadInput id="email" v-model="email" type="email" autocomplete="email" placeholder="n0call@example.com" />
          </div>
          <ShadButton type="submit" :disabled="busy">Save</ShadButton>
        </form>
        <p v-if="error" class="text-sm text-destructive" data-test="email-error">{{ error }}</p>
        <p v-if="message" class="text-sm text-green-600 dark:text-green-500" data-test="email-ok">{{ message }}</p>
      </template>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { badgeClass } from '@/lib/badge';
import { setEmail } from '@/services/password';
import { errorText } from '@/services/settings';
import { useSettingsStore } from '@/store';

const props = defineProps<{ current: string; verified: boolean }>();

const settingsStore = useSettingsStore();
const canEmail = computed(() => settingsStore.passwordResetEnabled);
const saved = ref(props.current);
const verified = ref(props.verified);
const email = ref(props.current);
const error = ref('');
const message = ref('');
const busy = ref(false);

onMounted(() => {
  void settingsStore.fetchConfig();
});

const save = async () => {
  error.value = '';
  message.value = '';
  const value = email.value.trim();
  if (value === saved.value) {
    return;
  }
  busy.value = true;
  try {
    message.value = await setEmail(value);
    saved.value = value;
    verified.value = false;
  } catch (err) {
    error.value = errorText(err, 'Could not save your email address.');
  } finally {
    busy.value = false;
  }
};
</script>
