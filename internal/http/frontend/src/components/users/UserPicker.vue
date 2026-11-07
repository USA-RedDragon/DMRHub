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
  <div class="flex flex-col gap-2">
    <ShadInput
      :id="inputId"
      v-model="query"
      type="search"
      placeholder="Callsign, username or DMR ID"
      aria-label="Find a user"
      :data-test="`${testPrefix}-search`"
    />
    <div class="max-h-56 overflow-y-auto rounded-md border border-border" role="radiogroup" :aria-label="label">
      <p v-if="matches.length === 0" class="px-3 py-2.5 text-sm text-muted-foreground" :data-test="`${testPrefix}-nomatch`">No user matches</p>
      <label
        v-for="u in matches"
        :key="u.id"
        class="flex cursor-pointer items-center gap-2.5 border-b border-border px-3 py-2.5 last:border-b-0 hover:bg-accent"
        :data-test="`${testPrefix}-user`"
      >
        <input v-model="picked" type="radio" :name="name" :value="u.id" class="size-4 accent-primary" />
        <span class="flex flex-col">
          <span><b class="font-mono">{{ u.callsign }}</b> {{ u.username }}</span>
          <span class="font-mono text-xs text-muted-foreground tabular-nums">DMR ID {{ u.id }}</span>
        </span>
      </label>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { Input as ShadInput } from '@/components/ui/input';
import type { PickableUser } from '@/services/accounts';

const props = withDefaults(defineProps<{
  users: PickableUser[];
  name?: string;
  label?: string;
  inputId?: string;
  testPrefix?: string;
}>(), {
  name: 'owner',
  label: 'Users',
  inputId: undefined,
  testPrefix: 'user-picker',
});

const picked = defineModel<number | null>({ default: null });
const query = ref('');

const matches = computed(() => {
  const q = query.value.trim().toLowerCase();
  if (!q) {
    return props.users;
  }
  return props.users.filter((u) => u.callsign.toLowerCase().includes(q) || u.username.toLowerCase().includes(q) || String(u.id).includes(q));
});
</script>
