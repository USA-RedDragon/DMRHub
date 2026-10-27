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
  <span v-if="badges.length > 0" class="inline-flex flex-wrap gap-1.5 align-middle" data-test="setting-badges">
    <span
      v-for="b in badges"
      :key="b.kind"
      class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2 text-xs font-medium leading-5"
      :class="b.tone"
      :data-test="`setting-badge-${b.kind}`"
    >
      <span class="size-1.5 shrink-0" :class="b.dot" />
      {{ b.text }}
    </span>
  </span>
</template>

<script lang="ts">
import { defineComponent, inject } from 'vue';
import { setupStateKey } from '@/lib/setupState';

type Badge = { kind: string; text: string; tone: string; dot: string };

export default defineComponent({
  props: {
    path: { type: String, required: true },
  },
  setup() {
    return { state: inject(setupStateKey, null) };
  },
  computed: {
    badges(): Badge[] {
      const s = this.state?.settings[this.path];
      if (!s) {
        return [];
      }
      const out: Badge[] = [];
      if (s.locked || (s.start_only && s.source !== 'generated' && s.source !== 'wizard')) {
        out.push({ kind: 'deployment', text: 'Set by the deployment', tone: 'border-border text-muted-foreground', dot: 'rounded-[2px] bg-muted-foreground' });
      }
      if (s.source === 'setup') {
        out.push({ kind: 'saved', text: 'Saved in Setup', tone: 'border-border text-foreground', dot: 'rounded-full bg-green-600 dark:bg-green-400' });
      }
      if (s.disagree) {
        out.push({ kind: 'differ', text: 'Replicas differ', tone: 'border-amber-500/60 text-amber-700 dark:text-amber-300', dot: 'rounded-full bg-amber-500' });
      }
      if (s.not_in_effect) {
        out.push({ kind: 'not-in-effect', text: 'Not in effect', tone: 'border-destructive/50 text-destructive', dot: 'rounded-full bg-destructive' });
      }
      return out;
    },
  },
});
</script>
