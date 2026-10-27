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
  <p v-if="startOnly" class="flex flex-wrap items-center gap-2" data-test="section-note">
    <SettingBadges :path="first" />
    <span class="text-muted-foreground">DMRHub reads these at start. Change them in the config file or the environment, then restart DMRHub.</span>
  </p>
</template>

<script lang="ts">
import { defineComponent, inject } from 'vue';
import SettingBadges from '@/components/setup/SettingBadges.vue';
import { setupStateKey } from '@/lib/setupState';

export default defineComponent({
  components: { SettingBadges },
  props: {
    prefix: { type: String, required: true },
  },
  setup() {
    return { state: inject(setupStateKey, null) };
  },
  computed: {
    paths(): string[] {
      const settings = this.state?.settings ?? {};
      return Object.keys(settings).filter((p) => p === this.prefix || p.startsWith(`${this.prefix}.`));
    },
    first(): string {
      return this.paths.find((p) => !this.state?.settings[p]?.locked) ?? this.paths[0] ?? this.prefix;
    },
    startOnly(): boolean {
      const settings = this.state?.settings ?? {};
      return this.paths.length > 0 && this.paths.every((p) => settings[p]?.start_only && !settings[p]?.editable);
    },
  },
});
</script>
