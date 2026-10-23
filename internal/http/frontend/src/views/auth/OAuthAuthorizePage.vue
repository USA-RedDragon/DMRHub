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
  <div class="mx-auto flex w-full max-w-[34rem] flex-col gap-4">
    <Card v-if="state === 'ask' && request">
      <CardHeader>
        <CardTitle>Allow this app to use your DMRHub account?</CardTitle>
        <p class="font-mono text-[0.95rem] font-semibold break-all" data-test="oauth-origin">{{ request.origin }}</p>
        <p class="text-sm text-muted-foreground">Signed in as {{ account }}</p>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <p class="text-sm">The app will be able to:</p>
        <ul class="m-0 grid list-none gap-2.5 p-0">
          <li
            v-for="scope in request.scopes"
            :key="scope.name"
            class="grid grid-cols-[1.25rem_1fr] items-start gap-2 text-[0.9rem]"
            data-test="oauth-scope"
          >
            <Check class="mt-0.5 size-4 text-muted-foreground" aria-hidden="true" />
            <span>{{ scope.description }}</span>
          </li>
        </ul>
        <p class="text-sm text-muted-foreground">
          It cannot change your password or account, and it never gets admin access. You can remove it at any time in
          Settings.
        </p>
        <div class="flex flex-wrap justify-end gap-2">
          <ShadButton variant="outline" type="button" :disabled="busy" data-test="oauth-deny" @click="answer(false)">
            Don't allow
          </ShadButton>
          <ShadButton type="button" :disabled="busy" data-test="oauth-allow" @click="answer(true)">Allow</ShadButton>
        </div>
        <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
      </CardContent>
    </Card>

    <Card v-else-if="state === 'allowed' || state === 'denied'">
      <CardContent class="pt-6">
        <p v-if="state === 'allowed'">Taking you back to {{ request?.origin }}.</p>
        <p v-else>You did not allow access. Taking you back to {{ request?.origin }}.</p>
      </CardContent>
    </Card>

    <Card v-else-if="state === 'invalid'">
      <CardHeader>
        <CardTitle>This app sent a request that DMRHub cannot accept.</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-2">
        <p class="text-sm text-muted-foreground">Go back to the app and try again, or ask its author for help.</p>
        <a v-if="back" :href="back" class="text-sm text-primary underline">Go back to the app</a>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useHead } from '@unhead/vue';
import { Check } from 'lucide-vue-next';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import API from '@/services/API';
import { answerAuthorize, getAuthorize, type OAuthRequest } from '@/services/oauth';
import { errorStatus } from '@/services/settings';
import { goTo } from '@/lib/navigate';

useHead({ title: 'Allow access' });

const route = useRoute();
const router = useRouter();

const state = ref<'loading' | 'ask' | 'allowed' | 'denied' | 'invalid'>('loading');
const request = ref<OAuthRequest | null>(null);
const me = ref<{ id: number; callsign: string } | null>(null);
const back = ref('');
const busy = ref(false);
const error = ref('');

const params = computed(() => {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(route.query)) {
    if (typeof v === 'string') {
      out[k] = v;
    }
  }
  return out;
});

const account = computed(() => (me.value ? `${me.value.callsign} (${me.value.id})` : ''));

const load = async () => {
  try {
    const res = await API.get('/users/me');
    me.value = res.data;
  } catch (err) {
    if (errorStatus(err) === 401) {
      router.replace({ path: '/login', query: { next: route.fullPath } });
      return;
    }
    state.value = 'invalid';
    return;
  }
  try {
    request.value = await getAuthorize(params.value);
    state.value = 'ask';
  } catch (err) {
    const data = (err as { response?: { data?: { redirect?: unknown } } })?.response?.data;
    back.value = typeof data?.redirect === 'string' ? data.redirect : '';
    state.value = 'invalid';
  }
};

const answer = async (approve: boolean) => {
  busy.value = true;
  error.value = '';
  try {
    const redirect = await answerAuthorize(params.value, approve);
    state.value = approve ? 'allowed' : 'denied';
    goTo(redirect);
  } catch {
    error.value = 'Could not send your answer. Try again.';
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>
