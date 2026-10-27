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
  <div v-if="setting" class="flex flex-col gap-1" data-test="setting-note">
    <small v-for="(line, i) in lines" :key="i" class="text-muted-foreground">{{ line }}</small>
    <ul
      v-if="setting.disagree && setting.replicas && setting.replicas.length > 0"
      class="mt-1 flex flex-col gap-0.5 rounded-md bg-amber-500/10 px-3 py-2 text-xs"
      :aria-label="`${label} on each replica`"
      data-test="setting-replicas"
    >
      <li v-for="r in setting.replicas" :key="r.replica" class="flex flex-wrap items-center gap-3">
        <span class="font-mono">{{ name(r.replica) }}</span>
        <span class="font-mono tabular-nums">{{ value(r.value) }}</span>
        <span>{{ source(r.source) }}<template v-if="r.self"> (this replica)</template></span>
        <SettingBadge v-if="notInEffect(r)" />
      </li>
    </ul>
  </div>
</template>

<script lang="ts">
import { defineComponent, h, inject } from 'vue';
import {
  deploymentPart, formatValue, replicaName, replicaNotInEffect, setupStateKey, settingLabel, sourceText,
  type ReplicaValue, type Setting,
} from '@/lib/setupState';

const SettingBadge = () => h('span', {
  class: 'inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border border-destructive/50 px-2 text-xs font-medium leading-5 text-destructive',
}, [h('span', { class: 'size-1.5 shrink-0 rounded-full bg-destructive' }), 'Not in effect']);

export default defineComponent({
  components: { SettingBadge },
  props: {
    path: { type: String, required: true },
  },
  setup() {
    return { state: inject(setupStateKey, null) };
  },
  computed: {
    setting(): Setting | undefined {
      return this.state?.settings[this.path];
    },
    label(): string {
      return settingLabel(this.path);
    },
    lines(): string[] {
      const s = this.setting;
      if (!s) {
        return [];
      }
      const out: string[] = [];
      if (s.source === 'generated') {
        out.push('DMRHub made a random value at its first start and keeps it in the database.');
      } else if (s.source === 'wizard') {
        out.push('The setup wizard set this. Change it in the config file or the environment of the deployment, then restart DMRHub.');
      } else if (s.locked) {
        let line = `The ${deploymentPart(s.source)} of the deployment sets this. Change it there and restart DMRHub.`;
        if (s.saved !== undefined) {
          line += ` A value saved in Setup, ${formatValue(s.saved)}, comes back if the deployment stops setting it.`;
        }
        out.push(line);
      } else if (s.start_only) {
        out.push('DMRHub reads this at start. Set it in the config file or the environment of the deployment, then restart DMRHub.');
      } else if (s.source === 'setup' && s.default !== undefined) {
        out.push(`Saved in Setup. Set it back to the default, ${formatValue(s.default)}, to follow the default.`);
      }
      if (s.not_in_effect) {
        out.push('This replica could not put this value into effect and keeps its previous value.');
      }
      return out;
    },
  },
  methods: {
    name: replicaName,
    value: formatValue,
    source: sourceText,
    notInEffect(r: ReplicaValue): boolean {
      return this.setting ? replicaNotInEffect(this.setting, r) : false;
    },
  },
});
</script>
