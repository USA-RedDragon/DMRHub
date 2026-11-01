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
  <Card data-test="tokens-card">
    <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-4">
      <div class="flex flex-col gap-1.5">
        <CardTitle>API tokens</CardTitle>
        <CardDescription>For your own scripts. A token acts as you. A password reset deletes every token.</CardDescription>
      </div>
      <ShadButton v-if="!adding" variant="outline" type="button" data-test="new-token" @click="startNew">New token</ShadButton>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <form
        v-if="adding"
        class="flex flex-col gap-4 rounded-lg border border-border p-4"
        data-test="token-form"
        novalidate
        @submit.prevent="make"
      >
        <div class="flex max-w-xs flex-col gap-1">
          <label for="token-name" class="text-sm font-medium">Name</label>
          <ShadInput id="token-name" v-model="name" autocomplete="off" :maxlength="64" placeholder="Net logger" />
        </div>
        <fieldset class="flex flex-col gap-1.5">
          <legend class="mb-1.5 text-sm font-medium">Access</legend>
          <label class="flex items-center gap-2.5 text-sm">
            <input v-model="access" type="radio" name="token-access" value="scoped" class="size-4 accent-primary" />
            Only what I pick
          </label>
          <label class="flex items-center gap-2.5 text-sm">
            <input v-model="access" type="radio" name="token-access" value="full" class="size-4 accent-primary" />
            Full account access
          </label>
        </fieldset>
        <fieldset v-if="access === 'scoped'" class="flex flex-col gap-1.5">
          <legend class="mb-1.5 text-sm font-medium">Can use</legend>
          <div class="grid grid-cols-[repeat(auto-fit,minmax(14rem,1fr))] gap-x-4 gap-y-2">
            <label v-for="scope in scopes" :key="scope.name" class="flex items-center gap-2.5 text-sm">
              <input v-model="picked" type="checkbox" :value="scope.name" class="size-4 shrink-0 accent-primary" />
              {{ scope.description }}
            </label>
          </div>
        </fieldset>
        <p
          v-else
          role="note"
          class="max-w-[70ch] rounded-md border-l-[3px] border-yellow-500 bg-yellow-500/10 px-3 py-2 text-sm"
          data-test="full-access-note"
        >
          <b class="font-semibold">Full account access</b> can do everything you can do signed in. It cannot open admin
          pages, also when you are an admin, and it cannot change account security: tokens, apps, password, username,
          callsign, email, or deleting the account.
        </p>
        <div class="flex max-w-[12.5rem] flex-col gap-1">
          <label for="token-expires" class="text-sm font-medium">Expires</label>
          <select id="token-expires" v-model.number="days" class="h-9 w-full rounded-md border border-input bg-background px-3 py-1.5 text-sm">
            <option v-for="d in expiries" :key="d" :value="d">{{ expiryText(d) }}</option>
          </select>
        </div>
        <div class="flex flex-wrap gap-2">
          <ShadButton type="submit" :disabled="busy">Make token</ShadButton>
          <ShadButton variant="outline" type="button" @click="adding = false">Cancel</ShadButton>
        </div>
      </form>
      <p v-if="error" class="text-sm text-destructive" data-test="token-error">{{ error }}</p>

      <div
        v-if="shown"
        class="flex flex-col gap-2 rounded-lg border border-primary bg-primary/5 px-4 py-3"
        data-test="token-secret"
      >
        <b class="text-sm font-semibold">{{ shown.rotated ? `Copy the new token of ${shown.name} now` : 'Copy the token now' }}</b>
        <div class="flex flex-wrap gap-2">
          <ShadInput
            :model-value="shown.secret"
            readonly
            aria-label="Token"
            class="min-w-0 flex-[1_1_16rem] font-mono"
            @focus="($event.target as HTMLInputElement).select()"
          />
          <CopyButton :text="shown.secret" class="h-9" />
        </div>
        <span class="text-xs text-muted-foreground">
          {{ shown.rotated ? 'The old token stopped working. DMRHub shows the new one only once.' : 'DMRHub shows it only once.' }}
        </span>
      </div>

      <Table v-if="tokens.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Can use</TableHead>
            <TableHead>Made</TableHead>
            <TableHead>Last used</TableHead>
            <TableHead>Expires</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="token in tokens" :key="token.id" :data-test="`token-${token.id}`">
            <TableCell>
              <div class="font-semibold">{{ token.name }}</div>
              <div class="font-mono text-xs text-muted-foreground">{{ token.prefix }}</div>
            </TableCell>
            <TableCell>
              <span v-if="token.full" :class="badgeClass('warn')" data-test="token-full">Full account access</span>
              <template v-else>{{ token.can_use }}</template>
            </TableCell>
            <TableCell class="whitespace-nowrap">{{ day(token.made) }}</TableCell>
            <TableCell class="whitespace-nowrap" :class="{ 'text-muted-foreground': !token.last_used }" data-test="token-last-used">
              {{ token.last_used ? when(token.last_used) : 'Never' }}
            </TableCell>
            <TableCell class="whitespace-nowrap" data-test="token-expires">
              <span v-if="token.expired && token.expires" :class="badgeClass('bad')">Expired {{ day(token.expires) }}</span>
              <template v-else>{{ token.expires ? day(token.expires) : 'Never' }}</template>
            </TableCell>
            <TableCell class="whitespace-nowrap">
              <div class="flex gap-2">
                <ShadButton variant="outline" size="sm" type="button" data-test="rotate" :disabled="busy" @click="rotate(token)">
                  Rotate
                </ShadButton>
                <ShadButton
                  variant="outline"
                  size="sm"
                  type="button"
                  class="text-destructive"
                  data-test="delete"
                  :disabled="busy"
                  @click="remove(token)"
                >
                  Delete
                </ShadButton>
              </div>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
      <p v-else-if="loaded && !adding" class="text-sm text-muted-foreground">No tokens yet.</p>
      <p v-if="deleted" class="text-sm text-green-600 dark:text-green-500">Deleted {{ deleted }}. It stopped working.</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { format } from 'date-fns';
import CopyButton from '@/components/enroll/CopyButton.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { badgeClass } from '@/lib/badge';
import type { OAuthScope } from '@/services/oauth';
import { errorText } from '@/services/settings';
import { deleteToken, getTokens, makeToken, rotateToken, type PersonalToken } from '@/services/tokens';

const defaultDays = 90;

const tokens = ref<PersonalToken[]>([]);
const scopes = ref<OAuthScope[]>([]);
const expiries = ref<number[]>([30, 90, 365, 0]);
const loaded = ref(false);
const busy = ref(false);
const error = ref('');
const deleted = ref('');
const shown = ref<{ name: string; secret: string; rotated: boolean } | null>(null);

const adding = ref(false);
const name = ref('');
const access = ref<'scoped' | 'full'>('scoped');
const picked = ref<string[]>([]);
const days = ref(defaultDays);

const day = (value: string) => format(new Date(value), 'MMM d, yyyy');
const when = (value: string) => format(new Date(value), 'MMM d, HH:mm');
const expiryText = (d: number) => {
  if (d === 0) {
    return 'Never';
  }
  if (d === 365) {
    return 'In 1 year';
  }
  return `In ${d} days`;
};

const clearNotes = () => {
  error.value = '';
  deleted.value = '';
  shown.value = null;
};

const load = async () => {
  try {
    const got = await getTokens();
    tokens.value = got.tokens;
    scopes.value = got.scopes;
    if (got.expiries.length > 0) {
      expiries.value = got.expiries;
    }
  } catch (err) {
    error.value = errorText(err, 'Could not load your tokens.');
  } finally {
    loaded.value = true;
  }
};

const startNew = () => {
  clearNotes();
  name.value = '';
  access.value = 'scoped';
  picked.value = [];
  days.value = defaultDays;
  adding.value = true;
};

const make = async () => {
  error.value = '';
  const full = access.value === 'full';
  if (name.value.trim() === '') {
    error.value = 'Enter a name.';
    return;
  }
  const chosen = scopes.value.map((s) => s.name).filter((n) => picked.value.includes(n));
  if (!full && chosen.length === 0) {
    error.value = 'Pick at least one thing the token can use, or full account access.';
    return;
  }
  busy.value = true;
  try {
    const made = await makeToken({ name: name.value.trim(), scopes: full ? [] : chosen, full, expires_in_days: days.value });
    tokens.value = [...tokens.value, made.token];
    shown.value = { name: made.token.name, secret: made.secret, rotated: false };
    adding.value = false;
  } catch (err) {
    error.value = errorText(err, 'Could not make the token.');
  } finally {
    busy.value = false;
  }
};

const rotate = async (token: PersonalToken) => {
  clearNotes();
  busy.value = true;
  try {
    const made = await rotateToken(token.id);
    tokens.value = tokens.value.map((t) => (t.id === token.id ? made.token : t));
    shown.value = { name: made.token.name, secret: made.secret, rotated: true };
  } catch (err) {
    error.value = errorText(err, `Could not rotate ${token.name}.`);
  } finally {
    busy.value = false;
  }
};

const remove = async (token: PersonalToken) => {
  clearNotes();
  busy.value = true;
  try {
    await deleteToken(token.id);
    tokens.value = tokens.value.filter((t) => t.id !== token.id);
    deleted.value = token.name;
  } catch (err) {
    error.value = errorText(err, `Could not delete ${token.name}.`);
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>
