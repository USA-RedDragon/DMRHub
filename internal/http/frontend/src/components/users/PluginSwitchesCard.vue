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
  <Card
    v-for="sw in switches"
    :key="`${sw.plugin_id}-${sw.key}`"
    :data-test="`plugin-switch-${sw.plugin_id}-${sw.key}`"
  >
    <CardHeader>
      <CardTitle>{{ sw.title }}</CardTitle>
      <CardDescription v-if="sw.description">{{ sw.description }}</CardDescription>
      <p class="text-xs text-muted-foreground" data-test="plugin-switch-plugin">From the plugin {{ sw.plugin }}.</p>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <div class="flex items-start justify-between gap-4">
        <div class="flex flex-col gap-1">
          <span :id="labelID(sw)" class="font-medium">{{ sw.title }}</span>
          <p class="text-sm" :class="sw.on ? '' : 'text-muted-foreground'" data-test="plugin-switch-status">
            {{ sw.on ? 'On.' : 'Off.' }}
          </p>
        </div>
        <Switch
          :aria-labelledby="labelID(sw)"
          :model-value="sw.on"
          :disabled="busy === keyOf(sw)"
          @update:model-value="(on: boolean) => save(sw, on, sw.choice?.value ?? '')"
        />
      </div>
      <div v-if="sw.on" class="flex flex-col gap-4" data-test="plugin-switch-details">
        <div v-if="sw.choice" class="flex flex-wrap items-center gap-2">
          <label :for="`${labelID(sw)}-choice`" class="text-sm">{{ sw.choice.label }}</label>
          <select
            :id="`${labelID(sw)}-choice`"
            :value="sw.choice.value"
            :disabled="busy === keyOf(sw)"
            class="h-9 rounded-md border border-input bg-background px-3 py-1.5 text-sm"
            data-test="plugin-switch-choice"
            @change="save(sw, true, ($event.target as HTMLSelectElement).value)"
          >
            <option v-for="o in sw.choice.options" :key="o" :value="o">{{ o }}</option>
          </select>
        </div>
        <dl
          v-if="sw.details.length > 0"
          class="m-0 grid grid-cols-[max-content_1fr] gap-x-[18px] gap-y-1 text-sm"
          data-test="plugin-switch-detail-list"
        >
          <template v-for="d in sw.details" :key="d.label">
            <dt class="text-muted-foreground">{{ d.label }}</dt>
            <dd class="m-0">
              <template v-for="(p, i) in codeParts(d.value)" :key="i">
                <span v-if="p.code" class="font-mono">{{ p.text }}</span>
                <template v-else>{{ p.text }}</template>
              </template>
            </dd>
          </template>
        </dl>
        <p
          v-if="sw.notice"
          class="max-w-[70ch] border-l-[3px] border-border py-1 pl-3.5 text-sm"
          data-test="plugin-switch-notice"
        >
          {{ sw.notice }}
        </p>
      </div>
      <p v-if="errors[keyOf(sw)]" class="text-sm text-destructive" data-test="plugin-switch-error">
        {{ errors[keyOf(sw)] }}
      </p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import { codeParts, getPluginSwitches, setPluginSwitch, type PluginSwitch } from '@/services/pluginSwitches';
import { errorText } from '@/services/settings';

const switches = ref<PluginSwitch[]>([]);
const busy = ref('');
const errors = ref<Record<string, string>>({});

const keyOf = (sw: PluginSwitch) => `${sw.plugin_id}-${sw.key}`;
const labelID = (sw: PluginSwitch) => `plugin-switch-${keyOf(sw)}`;

const save = async (sw: PluginSwitch, on: boolean, choice: string) => {
  const key = keyOf(sw);
  errors.value = { ...errors.value, [key]: '' };
  busy.value = key;
  try {
    const got = await setPluginSwitch(sw.plugin_id, sw.key, on, choice);
    switches.value = switches.value.map((s) => (keyOf(s) === key ? got : s));
  } catch (err) {
    errors.value = { ...errors.value, [key]: errorText(err, `Could not turn ${on ? 'on' : 'off'} ${sw.title}.`) };
  } finally {
    busy.value = '';
  }
};

onMounted(async () => {
  try {
    switches.value = await getPluginSwitches();
  } catch {
    switches.value = [];
  }
});
</script>
