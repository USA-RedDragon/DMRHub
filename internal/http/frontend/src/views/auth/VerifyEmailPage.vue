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
  <Card class="mx-auto w-full max-w-[34rem]">
    <CardHeader>
      <CardTitle>Confirm your email address</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <p v-if="state === 'loading'" class="text-sm text-muted-foreground">Checking the link.</p>
      <p v-else-if="state === 'done'" class="text-sm" data-test="verify-ok">{{ message }}</p>
      <p v-else class="text-sm text-destructive" data-test="verify-bad">{{ message }}</p>
      <RouterLink v-if="state !== 'loading'" to="/settings" class="text-sm text-primary underline">Go to Settings</RouterLink>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { useHead } from '@unhead/vue';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { verifyEmail } from '@/services/password';
import { errorText } from '@/services/settings';

useHead({ title: 'Confirm your email address' });

const badLink = 'This link does not work. It is older than 24 hours, was used already, or is for an older address.';

const state = ref<'loading' | 'done' | 'bad'>('loading');
const message = ref('');

onMounted(async () => {
  const token = new URLSearchParams(window.location.hash.slice(1)).get('token') ?? '';
  window.history.replaceState(window.history.state, '', window.location.pathname);
  if (!token) {
    message.value = badLink;
    state.value = 'bad';
    return;
  }
  try {
    message.value = await verifyEmail(token);
    state.value = 'done';
  } catch (err) {
    message.value = errorText(err, badLink);
    state.value = 'bad';
  }
});
</script>
