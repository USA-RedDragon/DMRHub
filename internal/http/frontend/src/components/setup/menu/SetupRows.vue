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
  <template v-for="(row, i) in rows" :key="i">
    <SetupField
      v-if="isField(row) && shown(row)"
      :field="row"
      :value="read(row.path)"
      :read="read"
      :setting="settings[row.path]"
      :error="errors[row.path]"
      :secret-set="secrets[row.path]"
      @set="forward"
    />
    <div v-else-if="isGrid(row) && row.grid.some(shown)" class="grid grid-cols-[repeat(auto-fit,minmax(230px,1fr))] gap-x-4.5">
      <template v-for="f in row.grid" :key="f.path">
        <SetupField
          v-if="shown(f)"
          :field="f"
          :value="read(f.path)"
          :read="read"
          :setting="settings[f.path]"
          :error="errors[f.path]"
          :secret-set="secrets[f.path]"
          @set="forward"
        />
      </template>
    </div>
    <details v-else-if="isDetails(row)" class="mb-5 rounded-lg border px-3.5 open:pb-3.5">
      <summary class="cursor-pointer py-2.5 font-semibold">{{ row.details }}</summary>
      <SetupRows :rows="row.rows" :read="read" :settings="settings" :errors="errors" :secrets="secrets" :only="only" @set="forward" />
    </details>
    <p v-else-if="isText(row)" class="-mt-2 mb-5 text-sm text-muted-foreground">
      {{ typeof row.text === 'function' ? row.text(read) : row.text }}
    </p>
  </template>
</template>

<script setup lang="ts">
import SetupField from '@/components/setup/menu/SetupField.vue';
import { isDetails, isField, isGrid, isText, type FieldDef, type Read, type Row } from '@/lib/setupMenu';
import type { Setting } from '@/lib/setupState';

defineOptions({ name: 'SetupRows' });

const props = defineProps<{
  rows: Row[];
  read: Read;
  settings: Record<string, Setting>;
  errors: Record<string, string>;
  secrets: Record<string, boolean>;
  only?: string[];
}>();

const emit = defineEmits<{ (e: 'set', path: string, value: unknown): void }>();

const shown = (f: FieldDef) => (!props.only || props.only.includes(f.path)) && (!f.show || f.show(props.read));

const forward = (path: string, value: unknown) => emit('set', path, value);
</script>
