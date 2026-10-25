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
  <Card class="mx-auto w-full max-w-[26rem]" data-test="forgot-password-card">
    <CardHeader>
      <CardTitle>Forgot your password?</CardTitle>
      <CardDescription>DMRHub emails you a link to choose a new one.</CardDescription>
    </CardHeader>
    <CardContent>
      <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <div class="flex min-w-0 flex-col gap-1">
          <label for="forgot-user" class="text-sm">Username or callsign</label>
          <ShadInput id="forgot-user" v-model="user" autocomplete="username" />
        </div>
        <div class="flex flex-wrap gap-2">
          <ShadButton type="submit" :disabled="sending">Email me a link</ShadButton>
        </div>
        <p v-if="sent" class="text-sm text-green-600 dark:text-green-500" data-test="forgot-sent">
          If that account has an email address, a link is on its way. It works for 1 hour.
        </p>
      </form>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useHead } from '@unhead/vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { forgotPassword } from '@/services/password';
import { errorText } from '@/services/settings';

useHead({ title: 'Forgot your password?' });

const user = ref('');
const sent = ref(false);
const sending = ref(false);

const submit = async () => {
  const name = user.value.trim();
  if (!name) {
    return;
  }
  sending.value = true;
  try {
    await forgotPassword(name);
    sent.value = true;
  } catch (err) {
    toast.error('Error', { description: errorText(err, 'Could not ask for a link.') });
  } finally {
    sending.value = false;
  }
};
</script>
