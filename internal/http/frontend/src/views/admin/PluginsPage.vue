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
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-end justify-between gap-4">
      <div class="flex flex-col gap-1.5">
        <h1 class="text-2xl font-semibold">Plugins</h1>
        <p class="text-sm text-muted-foreground">
          Programs that answer the calls to their own IDs, such as the parrot or the location server.
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <ShadButton as-child>
          <RouterLink to="/admin/plugins/upload" data-test="upload-plugin">Upload a plugin</RouterLink>
        </ShadButton>
        <ShadButton as-child variant="outline">
          <RouterLink to="/admin/plugins/new" data-test="add-plugin">Add an external plugin</RouterLink>
        </ShadButton>
      </div>
    </div>

    <p v-if="state === 'error'" class="text-sm text-destructive">Could not load the plugins.</p>

    <ul v-else-if="state === 'ready'" class="flex flex-col rounded-xl border sm:hidden" data-test="plugins-stacked">
      <li v-if="plugins.length === 0" class="px-4 py-6 text-sm text-muted-foreground">No plugins yet.</li>
      <li v-for="p in plugins" :key="p.id" class="flex items-start justify-between gap-3 border-b px-4 py-3 last:border-b-0">
        <div class="flex min-w-0 flex-col gap-0.5 text-sm">
          <b class="font-semibold">{{ p.name }}</b>
          <span><PluginBadge :kind="p.kind" /> <span v-if="p.version" class="font-mono">{{ p.version }}</span></span>
          <span>{{ formatRanges(p.ranges) }}</span>
          <span class="text-muted-foreground">{{ status(p, now) }} · {{ p.calls_today }} today</span>
        </div>
        <RouterLink :to="`/admin/plugins/${p.id}`" class="text-sm text-primary underline-offset-4 hover:underline">Edit</RouterLink>
      </li>
    </ul>

    <div v-if="state === 'ready'" class="hidden overflow-x-auto rounded-xl border sm:block">
      <table class="w-full border-collapse text-sm">
        <thead>
          <tr class="bg-muted/50 text-left text-muted-foreground">
            <th class="px-4 py-3 font-medium">Name</th>
            <th class="px-4 py-3 font-medium">Runs</th>
            <th class="px-4 py-3 font-medium">Version</th>
            <th class="px-4 py-3 font-medium">IDs</th>
            <th class="px-4 py-3 font-medium">Status</th>
            <th class="px-4 py-3 font-medium">Calls today</th>
            <th class="px-4 py-3 font-medium"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="plugins.length === 0">
            <td colspan="7" class="px-4 py-6 text-muted-foreground" data-test="no-plugins">No plugins yet.</td>
          </tr>
          <tr v-for="p in plugins" :key="p.id" class="border-t" :data-test="`plugin-${p.id}`">
            <td class="px-4 py-3.5 font-semibold">{{ p.name }}</td>
            <td class="px-4 py-3.5"><PluginBadge :kind="p.kind" /></td>
            <td class="px-4 py-3.5 font-mono">{{ p.version }}</td>
            <td class="px-4 py-3.5">{{ formatRanges(p.ranges) }}</td>
            <td class="px-4 py-3.5" :class="{ 'text-muted-foreground': !p.enabled }">
              <span class="inline-flex items-center gap-2">
                <span
                  v-if="p.enabled"
                  aria-hidden="true"
                  class="h-2 w-2 rounded-full"
                  :class="p.online ? 'bg-green-600' : 'border-2 border-zinc-400'"
                />
                {{ status(p, now) }}
              </span>
            </td>
            <td class="px-4 py-3.5 tabular-nums">{{ p.calls_today }}</td>
            <td class="px-4 py-3.5 text-right">
              <RouterLink :to="`/admin/plugins/${p.id}`" class="text-primary underline-offset-4 hover:underline">Edit</RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="state === 'ready'" role="note" class="max-w-[80ch] rounded-lg border bg-muted/40 px-3.5 py-3 text-sm" data-test="plugins-note">
      A plugin gets the calls to its IDs. It can send calls only from its IDs, to a radio or to a talkgroup among its IDs.
      Plugins that run inside DMRHub cannot reach your files, and reach only the hosts you allow on their page.
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { useHead } from '@unhead/vue';

import { Button as ShadButton } from '@/components/ui/button';
import PluginBadge from '@/components/PluginBadge.vue';
import { formatRanges, listPlugins, status, type AdminPlugin } from '@/services/plugins';

useHead({ title: 'Plugins' });

const state = ref<'loading' | 'ready' | 'error'>('loading');
const plugins = ref<AdminPlugin[]>([]);
const now = ref(Date.now());
let timer: ReturnType<typeof setInterval> | null = null;

const load = async () => {
  try {
    const list = await listPlugins();
    plugins.value = list.plugins;
    now.value = Date.now();
    state.value = 'ready';
  } catch {
    state.value = 'error';
  }
};

onMounted(() => {
  void load();
  timer = setInterval(() => void load(), 30000);
});

onUnmounted(() => {
  if (timer !== null) {
    clearInterval(timer);
  }
});
</script>
