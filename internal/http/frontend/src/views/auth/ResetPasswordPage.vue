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
  <Card class="mx-auto w-full max-w-[26rem]" data-test="reset-password-card">
    <CardHeader>
      <CardTitle>Choose a new password</CardTitle>
      <CardDescription v-if="callsign">For {{ callsign }}.</CardDescription>
    </CardHeader>
    <CardContent>
      <p v-if="state === 'bad'" class="text-sm text-destructive" data-test="reset-error">{{ error }}</p>
      <form v-else-if="state === 'ready'" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <div class="flex min-w-0 flex-col gap-1">
          <label for="reset-new" class="text-sm">New password</label>
          <ShadInput id="reset-new" v-model="next" type="password" autocomplete="new-password" />
        </div>
        <div class="flex min-w-0 flex-col gap-1">
          <label for="reset-confirm" class="text-sm">Confirm new password</label>
          <ShadInput id="reset-confirm" v-model="confirm" type="password" autocomplete="new-password" />
        </div>
        <p v-if="error" class="text-sm text-destructive" data-test="reset-error">{{ error }}</p>
        <div class="flex flex-wrap gap-2">
          <ShadButton type="submit" :disabled="saving || done">Set password</ShadButton>
        </div>
        <p v-if="done" class="text-sm text-green-600 dark:text-green-500" data-test="reset-done">
          Your password is set. Log in with it now.
        </p>
      </form>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useHead } from '@unhead/vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { checkResetLink, resetPassword } from '@/services/password';
import { errorText } from '@/services/settings';

useHead({ title: 'Choose a new password' });

const badLink = 'This link does not work. It is older than 1 hour or was used already. Ask for a new one.';

const state = ref<'loading' | 'ready' | 'bad'>('loading');
const token = ref('');
const callsign = ref('');
const next = ref('');
const confirm = ref('');
const error = ref('');
const saving = ref(false);
const done = ref(false);

onMounted(async () => {
  token.value = new URLSearchParams(window.location.hash.slice(1)).get('token') ?? '';
  window.history.replaceState(window.history.state, '', window.location.pathname);
  if (!token.value) {
    error.value = badLink;
    state.value = 'bad';
    return;
  }
  try {
    callsign.value = await checkResetLink(token.value);
    state.value = 'ready';
  } catch (err) {
    error.value = errorText(err, badLink);
    state.value = 'bad';
  }
});

const submit = async () => {
  error.value = '';
  if (!next.value) {
    error.value = 'Enter a new password.';
    return;
  }
  if (next.value !== confirm.value) {
    error.value = 'The new passwords do not match.';
    return;
  }
  saving.value = true;
  try {
    await resetPassword(token.value, next.value);
    next.value = '';
    confirm.value = '';
    done.value = true;
  } catch (err) {
    error.value = errorText(err, 'Could not set your password.');
  } finally {
    saving.value = false;
  }
};
</script>
