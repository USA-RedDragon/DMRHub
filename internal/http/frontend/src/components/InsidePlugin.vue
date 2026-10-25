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
  <Card>
    <CardHeader>
      <h2 class="flex flex-wrap items-center gap-2 text-lg font-semibold leading-none">
        {{ plugin.name }} <PluginBadge :kind="plugin.kind" />
      </h2>
      <p class="text-sm text-muted-foreground" data-test="inside-desc">{{ description }}</p>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div class="grid max-w-2xl gap-4">
        <div class="flex items-center gap-3">
          <Switch id="pp-on" v-model="enabled" aria-labelledby="pp-on-label" data-test="inside-on" @update:model-value="saved = false" />
          <label id="pp-on-label" for="pp-on" class="text-sm">On</label>
        </div>
        <div class="flex flex-col gap-1.5">
          <label for="pp-ids" class="text-sm font-medium">IDs</label>
          <ShadInput id="pp-ids" v-model="ids" />
          <p class="text-sm text-muted-foreground">Radio and talkgroup IDs, and ranges such as 9990-9999, separated by commas.</p>
          <p v-if="error" class="text-sm text-destructive" data-test="inside-error">{{ error }}</p>
        </div>
        <PluginHosts
          v-if="plugin.hosts.length > 0"
          v-model="allowed"
          :hosts="plugin.hosts"
          intro="The hosts this plugin asks to reach. It can reach only the ones you allow."
        />
        <div v-if="uploaded" class="flex flex-col gap-1.5">
          <label for="pp-file" class="text-sm font-medium">New version</label>
          <ShadInput id="pp-file" type="file" accept=".wasm" class="pt-1.5" @change="chooseFile" />
          <p class="text-sm text-muted-foreground">
            Upload a newer .wasm file of this plugin. It takes over when the current calls end. Hosts that the new version
            adds start out not allowed.
          </p>
        </div>
      </div>
      <div class="flex flex-wrap gap-2">
        <ShadButton type="button" :disabled="busy" data-test="inside-save" @click="save">Save</ShadButton>
        <ShadButton v-if="uploaded" type="button" variant="destructive" :disabled="busy" data-test="inside-remove" @click="confirming = true">
          Remove plugin
        </ShadButton>
      </div>
      <p v-if="saved" class="text-sm text-green-600 dark:text-green-500" data-test="inside-saved">Saved.</p>
      <div
        v-if="confirming"
        role="alert"
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-yellow-600/40 bg-yellow-600/10 px-3.5 py-3 text-sm"
        data-test="inside-confirm"
      >
        <span>Remove {{ plugin.name }}? Calls to {{ formatRanges(plugin.ranges) }} will go nowhere.</span>
        <div class="flex gap-2">
          <ShadButton type="button" variant="destructive" size="sm" :disabled="busy" data-test="inside-yes" @click="remove">Remove</ShadButton>
          <ShadButton type="button" variant="outline" size="sm" data-test="inside-keep" @click="confirming = false">Keep it</ShadButton>
        </div>
      </div>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';

import PluginBadge from '@/components/PluginBadge.vue';
import PluginHosts from '@/components/PluginHosts.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import {
  deletePlugin,
  formatIDs,
  formatRanges,
  parseIDs,
  updatePlugin,
  uploadPluginVersion,
  type AdminPlugin,
} from '@/services/plugins';
import { errorText } from '@/services/settings';

const props = defineProps<{ initial: AdminPlugin }>();

const router = useRouter();

const plugin = ref<AdminPlugin>(props.initial);
const enabled = ref(true);
const ids = ref('');
const allowed = ref<string[]>([]);
const file = ref<File | null>(null);
const error = ref('');
const saved = ref(false);
const busy = ref(false);
const confirming = ref(false);

const fill = (p: AdminPlugin) => {
  plugin.value = p;
  enabled.value = p.enabled;
  ids.value = formatIDs(p.ranges);
  allowed.value = p.hosts.filter((h) => h.allowed).map((h) => h.address);
};
fill(props.initial);


const uploaded = computed(() => plugin.value.kind === 'uploaded');
const description = computed(() => {
  const version = plugin.value.version ? `Version ${plugin.value.version}.` : '';
  return [plugin.value.description, version].filter((s) => s !== '').join(' ');
});

const chooseFile = (e: Event) => {
  file.value = (e.target as HTMLInputElement).files?.[0] ?? null;
};

const save = async () => {
  error.value = '';
  const ranges = parseIDs(ids.value);
  if (ranges === null) {
    error.value = 'Use IDs and ranges such as 9990, 5050-5059.';
    return;
  }
  busy.value = true;
  try {
    if (file.value) {
      fill(await uploadPluginVersion(plugin.value.id, file.value));
      file.value = null;
    }
    const p = await updatePlugin(plugin.value.id, { name: plugin.value.name, enabled: enabled.value, ranges, allowed_hosts: allowed.value });
    fill(p);
    saved.value = true;
  } catch (err) {
    error.value = errorText(err, 'Could not save. Try again later.');
  } finally {
    busy.value = false;
  }
};

const remove = async () => {
  busy.value = true;
  try {
    await deletePlugin(plugin.value.id);
    confirming.value = false;
    await router.push('/admin/plugins');
  } catch (err) {
    confirming.value = false;
    error.value = errorText(err, 'Could not delete. Try again later.');
  } finally {
    busy.value = false;
  }
};
</script>
