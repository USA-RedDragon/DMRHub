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
  <InsidePlugin v-if="state === 'ready' && saved && isInside(saved)" :initial="saved" />
  <div v-else class="flex flex-col gap-6">
    <div class="flex flex-col gap-1.5">
      <RouterLink to="/admin/plugins" class="text-sm text-primary underline-offset-4 hover:underline">Plugins</RouterLink>
      <h1 class="text-2xl font-semibold">{{ isNew ? 'Add a plugin' : saved?.name }}</h1>
      <p v-if="saved" class="text-sm text-muted-foreground" data-test="plugin-subtitle">
        {{ status(saved) }} · {{ saved.calls_today }} {{ saved.calls_today === 1 ? 'call' : 'calls' }} today
      </p>
    </div>

    <p v-if="state === 'missing'" class="text-sm" data-test="plugin-missing">No such plugin.</p>
    <p v-else-if="state === 'error'" class="text-sm text-destructive">Could not load the plugin.</p>

    <div v-else-if="state === 'ready'" class="grid items-start gap-6 lg:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>Plugin</CardTitle>
        </CardHeader>
        <CardContent>
          <form class="flex flex-col gap-5" @submit.prevent="save">
            <div class="flex flex-col gap-1.5">
              <label for="plugin-name" class="text-sm font-medium">Name</label>
              <ShadInput id="plugin-name" v-model="form.name" maxlength="64" />
            </div>
            <fieldset class="flex flex-col gap-2">
              <legend class="mb-2 text-sm font-medium">IDs</legend>
              <div v-for="(r, i) in form.ranges" :key="i" class="flex flex-wrap items-center gap-2" :data-test="`range-${i}`">
                <label :for="`from-${i}`" class="text-sm text-muted-foreground">From</label>
                <ShadInput :id="`from-${i}`" v-model="r.first" inputmode="numeric" class="w-32" />
                <label :for="`to-${i}`" class="text-sm text-muted-foreground">to</label>
                <ShadInput :id="`to-${i}`" v-model="r.last" inputmode="numeric" class="w-32" />
                <ShadButton type="button" variant="outline" size="sm" :data-test="`remove-${i}`" @click="form.ranges.splice(i, 1)">Remove</ShadButton>
              </div>
              <div>
                <ShadButton type="button" variant="outline" size="sm" data-test="add-range" @click="form.ranges.push({ first: '', last: '' })">
                  Add a range
                </ShadButton>
              </div>
              <span class="text-[13px] text-muted-foreground">
                Private calls to these IDs go only to the plugin. Group calls to a talkgroup among them still reach its
                subscribers, and the plugin gets a copy. IDs may not overlap another plugin's.
              </span>
            </fieldset>
            <div class="flex items-center justify-between gap-4">
              <div class="flex flex-col gap-0.5">
                <span id="plugin-on-label" class="text-sm font-medium">Plugin on</span>
                <span class="text-[13px] text-muted-foreground">When off, the plugin cannot connect and gets no calls.</span>
              </div>
              <Switch v-model="form.enabled" aria-labelledby="plugin-on-label" data-test="plugin-on" />
            </div>
            <div class="flex items-center gap-3">
              <ShadButton type="submit" :disabled="busy" data-test="save">Save</ShadButton>
              <span v-if="message" class="text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'" data-test="save-message">{{ message }}</span>
            </div>
          </form>
        </CardContent>
      </Card>

      <div v-if="saved" class="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Connection</CardTitle>
          </CardHeader>
          <CardContent class="flex flex-col gap-4">
            <dl class="grid grid-cols-[140px_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-sm" data-test="connection">
              <dt class="text-muted-foreground">Address</dt>
              <dd class="font-mono">{{ connection?.address || 'Set the URL of the hub in Setup' }}</dd>
              <template v-if="connection?.own_certificate && connection.fingerprint">
                <dt class="text-muted-foreground">Certificate</dt>
                <dd class="break-all font-mono">SHA-256 {{ connection.fingerprint }}</dd>
              </template>
              <dt class="text-muted-foreground">Token</dt>
              <dd>Made on {{ formatDay(saved.token_created_at) }}</dd>
            </dl>
            <div>
              <ShadButton type="button" variant="outline" size="sm" :disabled="busy" data-test="new-token" @click="rotate">Make a new token</ShadButton>
            </div>
            <span class="text-[13px] text-muted-foreground">
              A new token replaces the old one, and the plugin is disconnected until it uses the new one.
            </span>
          </CardContent>
        </Card>
        <Card class="border-red-200 dark:border-red-900">
          <CardHeader>
            <CardTitle>Delete this plugin</CardTitle>
          </CardHeader>
          <CardContent class="flex flex-col gap-3">
            <p class="text-sm text-muted-foreground">The plugin is disconnected and its IDs are free again.</p>
            <div>
              <ShadButton type="button" variant="destructive" size="sm" :disabled="busy" data-test="delete" @click="remove">Delete</ShadButton>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>

    <div v-if="token" class="fixed inset-0 z-50 flex items-center justify-center bg-zinc-900/45 p-4">
      <div role="dialog" aria-modal="true" aria-labelledby="token-title" class="flex w-full max-w-[600px] flex-col gap-4 rounded-xl bg-background p-7 shadow-xl" data-test="token-dialog">
        <div class="flex flex-col gap-1.5">
          <h2 id="token-title" class="text-lg font-semibold">Copy the token of {{ saved?.name }}</h2>
          <p class="text-sm text-muted-foreground">This is the only time it is shown. The plugin sends it when it connects.</p>
        </div>
        <div class="flex gap-2">
          <label for="plugin-token" class="sr-only">Token</label>
          <ShadInput id="plugin-token" ref="tokenInput" :model-value="token" readonly class="min-w-0 grow bg-muted/40 font-mono" />
          <ShadButton type="button" variant="outline" data-test="copy-token" @click="copy">{{ copied ? 'Copied' : 'Copy' }}</ShadButton>
        </div>
        <dl class="grid grid-cols-[110px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
          <dt class="text-muted-foreground">Address</dt>
          <dd class="font-mono">{{ connection?.address || 'Set the URL of the hub in Setup' }}</dd>
          <template v-if="connection?.own_certificate && connection.fingerprint">
            <dt class="text-muted-foreground">Certificate</dt>
            <dd class="break-all font-mono">SHA-256 {{ connection.fingerprint }}</dd>
          </template>
        </dl>
        <div class="flex justify-end">
          <ShadButton type="button" data-test="token-done" @click="done">Done</ShadButton>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { RouterLink, useRoute, useRouter } from 'vue-router';
import { useHead } from '@unhead/vue';

import InsidePlugin from '@/components/InsidePlugin.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import {
  addPlugin,
  deletePlugin,
  formatDay,
  isInside,
  listPlugins,
  newPluginToken,
  status,
  updatePlugin,
  type AdminPlugin,
  type PluginConnection,
  type PluginRange,
} from '@/services/plugins';
import { errorText } from '@/services/settings';

const route = useRoute();
const router = useRouter();
const isNew = computed(() => route.params.id === 'new');

useHead({ title: 'Plugin' });

type RangeText = { first: string; last: string };

const state = ref<'loading' | 'ready' | 'missing' | 'error'>('loading');
const saved = ref<AdminPlugin | null>(null);
const connection = ref<PluginConnection | null>(null);
const form = reactive<{ name: string; enabled: boolean; ranges: RangeText[] }>({ name: '', enabled: true, ranges: [{ first: '', last: '' }] });
const busy = ref(false);
const message = ref('');
const failed = ref(false);
const token = ref('');
const copied = ref(false);

const fill = (p: AdminPlugin) => {
  saved.value = p;
  form.name = p.name;
  form.enabled = p.enabled;
  form.ranges = p.ranges.map((r) => ({ first: String(r.first), last: String(r.last) }));
};

const ranges = (): PluginRange[] | null => {
  const out: PluginRange[] = [];
  for (const r of form.ranges) {
    if (r.first.trim() === '' && r.last.trim() === '') {
      continue;
    }
    const first = Number(r.first);
    const last = r.last.trim() === '' ? first : Number(r.last);
    if (!Number.isInteger(first) || !Number.isInteger(last)) {
      return null;
    }
    out.push({ first, last });
  }
  return out;
};

const fail = (text: string) => {
  failed.value = true;
  message.value = text;
};

const save = async () => {
  message.value = '';
  failed.value = false;
  const rs = ranges();
  if (rs === null) {
    fail('IDs are whole numbers.');
    return;
  }
  busy.value = true;
  try {
    if (isNew.value) {
      const made = await addPlugin({ name: form.name.trim(), enabled: form.enabled, ranges: rs });
      let p = made.plugin;
      if (!form.enabled) {
        p = await updatePlugin(p.id, { name: p.name, enabled: false, ranges: rs });
      }
      fill(p);
      token.value = made.token;
    } else if (saved.value) {
      fill(await updatePlugin(saved.value.id, { name: form.name.trim(), enabled: form.enabled, ranges: rs }));
      message.value = 'Saved.';
    }
  } catch (err) {
    fail(errorText(err, 'Could not save. Try again later.'));
  } finally {
    busy.value = false;
  }
};

const rotate = async () => {
  if (!saved.value) {
    return;
  }
  busy.value = true;
  message.value = '';
  try {
    const made = await newPluginToken(saved.value.id);
    fill(made.plugin);
    token.value = made.token;
  } catch (err) {
    fail(errorText(err, 'Could not make a new token. Try again later.'));
  } finally {
    busy.value = false;
  }
};

const remove = async () => {
  if (!saved.value || !window.confirm(`Delete ${saved.value.name}? Its IDs are free again.`)) {
    return;
  }
  busy.value = true;
  try {
    await deletePlugin(saved.value.id);
    await router.push('/admin/plugins');
  } catch (err) {
    fail(errorText(err, 'Could not delete. Try again later.'));
  } finally {
    busy.value = false;
  }
};

const copy = async () => {
  try {
    await navigator.clipboard.writeText(token.value);
    copied.value = true;
  } catch {
    copied.value = false;
  }
};

const done = async () => {
  const id = saved.value?.id;
  token.value = '';
  copied.value = false;
  if (isNew.value && id !== undefined) {
    await router.replace(`/admin/plugins/${id}`);
  }
};

onMounted(async () => {
  try {
    const list = await listPlugins();
    connection.value = list.connection;
    if (!isNew.value) {
      const p = list.plugins.find((x) => String(x.id) === String(route.params.id));
      if (!p) {
        state.value = 'missing';
        return;
      }
      fill(p);
    }
    state.value = 'ready';
  } catch {
    state.value = 'error';
  }
});
</script>
