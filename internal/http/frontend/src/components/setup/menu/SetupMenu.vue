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
  <div class="grid items-start gap-7 md:grid-cols-[240px_minmax(0,1fr)]">
    <nav
      class="flex gap-1 overflow-x-auto rounded-xl border bg-sidebar p-1.5 md:sticky md:top-4 md:block md:max-h-[calc(100vh-2rem)] md:overflow-y-auto md:px-2 md:py-2.5"
      aria-label="Setup areas"
      data-test="setup-menu"
    >
      <template v-for="g in groups" :key="g.name">
        <h4 class="mx-2.5 mt-2.5 mb-1 hidden text-[11px] font-bold tracking-[0.06em] text-muted-foreground uppercase first:mt-0.5 md:block">
          {{ g.name }}
        </h4>
        <RouterLink
          v-for="t in g.tabs"
          :key="t.id"
          :to="{ hash: `#${t.id}` }"
          class="no-underline flex items-center gap-2 rounded-[7px] px-2.5 py-1.5 font-medium whitespace-nowrap text-foreground hover:bg-sidebar-accent"
          :class="t.id === tab.id ? 'bg-sidebar-accent font-bold shadow-[inset_3px_0_0_var(--primary)]' : ''"
          :aria-current="t.id === tab.id ? 'page' : undefined"
          :data-test="`menu-${t.id}`"
        >
          {{ t.name }}
          <span
            v-if="marks[t.id] === 'error'"
            class="ml-auto size-2 shrink-0 rounded-full bg-destructive-foreground"
            title="Needs a fix"
            :data-test="`menu-error-${t.id}`"
          ><span class="sr-only">Needs a fix</span></span>
          <span
            v-else-if="marks[t.id] === 'dirty'"
            class="ml-auto size-2 shrink-0 rounded-full bg-amber-500"
            title="Unsaved changes"
            :data-test="`menu-dirty-${t.id}`"
          ><span class="sr-only">Unsaved changes</span></span>
          <span
            v-else-if="startOnly(t)"
            class="ml-auto hidden text-[11px] font-medium text-muted-foreground md:inline"
          >Read at start</span>
        </RouterLink>
      </template>
    </nav>
    <section :data-test="`tab-${tab.id}`" :aria-labelledby="`tab-title-${tab.id}`">
      <div class="mb-3.5 flex flex-wrap items-end justify-between gap-2">
        <h2 :id="`tab-title-${tab.id}`" class="text-[22px] leading-tight font-bold">{{ tab.name }}</h2>
      </div>
      <p v-if="tab.intro.length > 0" class="mb-4 max-w-[70ch] text-muted-foreground">
        <template v-for="(part, i) in tab.intro" :key="i">
          <template v-if="typeof part === 'string'">{{ part }}</template>
          <RouterLink v-else :to="part.to">{{ part.text }}</RouterLink>
        </template>
      </p>
      <div
        v-if="startOnly(tab)"
        class="mb-4 flex items-start gap-2.5 rounded-[10px] border bg-muted px-3.5 py-2.5 text-sm"
        data-test="section-note"
      >
        <span class="inline-flex items-center gap-1.5 rounded-full border px-2 text-xs font-semibold leading-5 whitespace-nowrap text-muted-foreground">
          <span class="size-1.5 rounded-[2px] border border-muted-foreground" />Read at start
        </span>
        <span>{{ startNote }}</span>
      </div>
      <template v-if="state.loaded">
        <TranscodingTab
          v-if="tab.custom === 'transcoding'"
          :value="read(AMBE_SERVERS)"
          :setting="state.settings[AMBE_SERVERS]"
          :error="errors[AMBE_SERVERS]"
          :saves="saves"
          :wizard="wizard"
          @set="forward"
        />
        <SetupCard
          v-for="c in tab.cards"
          v-else
          :key="c.title"
          :card="c"
          :read="read"
          :settings="state.settings"
          :errors="errors"
          :secrets="secrets"
          :only="tab.optional ? Object.keys(state.settings) : undefined"
          :wizard="wizard"
          @set="forward"
        />
        <slot name="tab" />
      </template>
      <slot name="savebar" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { RouterLink } from 'vue-router';
import SetupCard from '@/components/setup/menu/SetupCard.vue';
import TranscodingTab from '@/components/setup/menu/TranscodingTab.vue';
import {
  AMBE_SERVERS, tabReadOnly, tabStartOnly, type Read, type TabDef, type TabGroup, type TabMark,
} from '@/lib/setupMenu';
import type { SetupState } from '@/lib/setupState';

const props = defineProps<{
  tabs: TabDef[];
  tab: TabDef;
  state: SetupState;
  read: Read;
  errors: Record<string, string>;
  secrets: Record<string, boolean>;
  saves: number;
  marks: Record<string, TabMark>;
  wizard?: boolean;
}>();

const emit = defineEmits<{ (e: 'set', path: string, value: unknown): void }>();

const groupNames: TabGroup[] = ['Server', 'Users', 'DMR', 'Features'];

const groups = computed(() =>
  groupNames.map((name) => ({ name, tabs: props.tabs.filter((t) => t.group === name) })).filter((g) => g.tabs.length > 0));

const startOnly = (t: TabDef): boolean => tabStartOnly(t, props.state) && tabReadOnly(t, props.state);

const startNote = computed(() => (props.wizard
  ? 'DMRHub reads these at start, and the setup wizard changes them only while DMRHub cannot reach its database. Change them in the config file or the environment, then restart DMRHub.'
  : 'DMRHub reads these at start. Change them in the config file or the environment, then restart DMRHub.'));

const forward = (path: string, value: unknown) => emit('set', path, value);
</script>
