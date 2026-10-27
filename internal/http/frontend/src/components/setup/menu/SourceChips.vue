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
  <span
    v-for="c in chips"
    :key="c.kind"
    class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2 text-xs font-semibold leading-5"
    :class="tone(c.kind)"
    :title="c.kind === 'lock' ? 'Set by the deployment' : undefined"
    :data-test="`source-chip-${c.kind}`"
  >
    <Lock v-if="c.kind === 'lock'" class="size-3" aria-hidden="true" />
    <span v-else class="size-1.5 shrink-0" :class="dot(c.kind)" />
    {{ c.text }}
    <code v-if="c.code" class="font-mono text-xs">{{ c.code }}</code>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { Lock } from 'lucide-vue-next';
import { sourceChips, type ChipKind } from '@/lib/setupMenu';
import type { Setting } from '@/lib/setupState';

const props = defineProps<{ setting?: Setting }>();

const chips = computed(() => sourceChips(props.setting));

const tone = (k: ChipKind): string => {
  switch (k) {
    case 'setup':
      return 'border-border text-foreground';
    case 'differ':
      return 'border-amber-500/60 text-amber-700 dark:text-amber-300';
    case 'not-in-effect':
      return 'border-destructive/50 text-destructive-foreground';
    default:
      return 'border-border text-muted-foreground';
  }
};

const dot = (k: ChipKind): string => {
  switch (k) {
    case 'setup':
      return 'rounded-full bg-green-600 dark:bg-green-400';
    case 'boot':
      return 'rounded-[2px] border border-muted-foreground';
    case 'generated':
    case 'wizard':
      return 'rounded-[2px] bg-muted-foreground';
    case 'differ':
      return 'rounded-full bg-amber-500';
    case 'not-in-effect':
      return 'rounded-full bg-destructive-foreground';
    default:
      return 'rounded-full bg-muted-foreground';
  }
};
</script>
