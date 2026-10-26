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
      <h2 class="text-lg font-semibold leading-none">Upload a plugin</h2>
      <p class="text-sm text-muted-foreground">A plugin is one .wasm file. DMRHub runs it inside itself, on any computer DMRHub runs on.</p>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div class="grid max-w-2xl gap-4">
        <div class="flex flex-col gap-1.5">
          <label for="up-file" class="text-sm font-medium">Plugin file</label>
          <ShadInput id="up-file" type="file" accept=".wasm" class="pt-1.5" @change="choose" />
          <p v-for="(m, i) in fileErrors" :key="i" class="text-sm text-destructive" data-test="upload-error">{{ m }}</p>
        </div>
        <div v-if="manifest" class="flex flex-col gap-4" data-test="upload-details">
          <dl class="grid grid-cols-[110px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
            <dt class="text-muted-foreground">Name</dt>
            <dd>{{ manifest.name }}</dd>
            <dt class="text-muted-foreground">Version</dt>
            <dd class="font-mono">{{ manifest.version }}</dd>
            <dt class="text-muted-foreground">Author</dt>
            <dd>{{ manifest.author }}</dd>
          </dl>
          <p v-if="manifest.description" class="text-sm text-muted-foreground">{{ manifest.description }}</p>
          <div class="flex flex-col gap-1.5">
            <label for="up-ids" class="text-sm font-medium">IDs</label>
            <ShadInput id="up-ids" v-model="ids" />
            <p class="text-sm text-muted-foreground">
              The plugin asks for these. Radio and talkgroup IDs, and ranges such as 9990-9999, separated by commas.
            </p>
            <p v-for="(m, i) in idErrors" :key="i" class="text-sm text-destructive" data-test="upload-ids-error">{{ m }}</p>
            <p v-if="saveError" class="text-sm text-destructive" data-test="upload-save-error">{{ saveError }}</p>
          </div>
          <PluginHosts
            v-if="manifest.hosts.length > 0"
            v-model="allowed"
            :hosts="manifest.hosts"
            intro="The plugin asks to reach these hosts. It can reach only the ones you allow."
          />
          <div v-if="hostErrors.length > 0" class="flex flex-col gap-1.5">
            <span v-if="manifest.hosts.length === 0" class="text-sm font-medium">Network</span>
            <p v-for="(m, i) in hostErrors" :key="i" class="text-sm text-destructive" data-test="upload-hosts-error">{{ m }}</p>
          </div>
        </div>
      </div>
      <div>
        <ShadButton type="button" :disabled="!manifest || busy || fileErrors.length > 0 || hostErrors.length > 0" data-test="upload-go" @click="upload">
          Upload and start
        </ShadButton>
      </div>
      <p v-if="done" class="text-sm text-green-600 dark:text-green-500" data-test="upload-done">{{ done }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useHead } from '@unhead/vue';

import PluginHosts from '@/components/PluginHosts.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import {
  formatIDs,
  inspectPlugin,
  parseIDs,
  problemsFor,
  refusalOf,
  uploadPlugin,
  type PluginManifest,
  type PluginProblem,
} from '@/services/plugins';
import { errorText } from '@/services/settings';

useHead({ title: 'Upload a plugin' });

const notAPlugin = 'Choose a .wasm plugin file.';
const notWasm = "This file is not a WebAssembly module. Choose the .wasm file that the plugin's build made.";

const file = ref<File | null>(null);
const manifest = ref<PluginManifest | null>(null);
const fileError = ref('');
const problems = ref<PluginProblem[]>([]);
const saveError = ref('');
const ids = ref('');
const allowed = ref<string[]>([]);
const busy = ref(false);
const done = ref('');

const fileErrors = computed(() => [...(fileError.value ? [fileError.value] : []), ...problemsFor(problems.value, 'file')]);
const idErrors = computed(() => problemsFor(problems.value, 'ids'));
const hostErrors = computed(() => problemsFor(problems.value, 'hosts'));

const show = (m: PluginManifest, f: File) => {
  file.value = f;
  manifest.value = m;
  ids.value = formatIDs(m.ranges);
  allowed.value = [];
};

const choose = async (e: Event) => {
  const f = (e.target as HTMLInputElement).files?.[0] ?? null;
  manifest.value = null;
  fileError.value = '';
  problems.value = [];
  saveError.value = '';
  done.value = '';
  file.value = null;
  if (!f) {
    return;
  }
  if (!/\.wasm$/i.test(f.name)) {
    fileError.value = notWasm;
    return;
  }
  busy.value = true;
  try {
    show(await inspectPlugin(f), f);
  } catch (err) {
    const refusal = refusalOf(err);
    if (refusal) {
      problems.value = refusal.problems;
      if (refusal.manifest) {
        show(refusal.manifest, f);
      }
    } else {
      fileError.value = errorText(err, notAPlugin);
    }
  } finally {
    busy.value = false;
  }
};

const upload = async () => {
  if (!file.value || !manifest.value) {
    return;
  }
  saveError.value = '';
  problems.value = [];
  const ranges = parseIDs(ids.value);
  if (ranges === null) {
    saveError.value = 'Use IDs and ranges such as 9990, 5050-5059.';
    return;
  }
  busy.value = true;
  try {
    const p = await uploadPlugin(file.value, ranges, allowed.value);
    done.value = `${p.name} is running.`;
  } catch (err) {
    const refusal = refusalOf(err);
    if (refusal) {
      problems.value = refusal.problems;
    } else {
      saveError.value = errorText(err, 'Could not save. Try again later.');
    }
  } finally {
    busy.value = false;
  }
};
</script>
