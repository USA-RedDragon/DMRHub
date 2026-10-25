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
  <Card data-test="password-card">
    <CardHeader>
      <CardTitle>Password</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <p
        v-if="mustChange"
        role="alert"
        class="rounded-lg border border-yellow-500/50 bg-yellow-500/10 px-4 py-3 text-sm"
        data-test="password-must-change"
      >
        An admin set a temporary password for you. Choose your own password to keep using DMRHub.
      </p>
      <form class="flex max-w-lg flex-col gap-4" novalidate @submit.prevent="submit">
        <div class="flex min-w-0 flex-col gap-1">
          <label for="pw-current" class="text-sm">Current password</label>
          <ShadInput id="pw-current" v-model="current" type="password" autocomplete="current-password" />
        </div>
        <div class="flex min-w-0 flex-col gap-1">
          <label for="pw-new" class="text-sm">New password</label>
          <ShadInput id="pw-new" v-model="next" type="password" autocomplete="new-password" />
        </div>
        <div class="flex min-w-0 flex-col gap-1">
          <label for="pw-confirm" class="text-sm">Confirm new password</label>
          <ShadInput id="pw-confirm" v-model="confirm" type="password" autocomplete="new-password" />
        </div>
        <p v-if="error" class="text-sm text-destructive" data-test="password-error">{{ error }}</p>
        <div class="flex flex-wrap gap-2">
          <ShadButton type="submit" :disabled="saving">Change password</ShadButton>
        </div>
        <p v-if="done" class="text-sm text-green-600 dark:text-green-500" data-test="password-ok">
          Your password is changed. Your other sessions are logged out.
        </p>
      </form>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { changePassword } from '@/services/password';
import { errorText } from '@/services/settings';

const props = defineProps<{ userId: number; mustChange?: boolean }>();
const emit = defineEmits<{ changed: [] }>();

const current = ref('');
const next = ref('');
const confirm = ref('');
const error = ref('');
const done = ref(false);
const saving = ref(false);

const check = (): string => {
  if (!current.value) {
    return 'Enter your current password.';
  }
  if (!next.value) {
    return 'Enter a new password.';
  }
  if (next.value !== confirm.value) {
    return 'The new passwords do not match.';
  }
  return '';
};

const submit = async () => {
  done.value = false;
  error.value = check();
  if (error.value) {
    return;
  }
  saving.value = true;
  try {
    await changePassword(props.userId, current.value, next.value);
    current.value = '';
    next.value = '';
    confirm.value = '';
    done.value = true;
    emit('changed');
  } catch (err) {
    error.value = errorText(err, 'Could not change your password.');
  } finally {
    saving.value = false;
  }
};
</script>
