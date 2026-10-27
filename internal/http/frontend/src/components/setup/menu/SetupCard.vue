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
  <Card v-if="hasContent" :class="nested ? 'mb-3.5 gap-3.5 bg-transparent py-4.5 shadow-none' : 'mb-5'" :data-test="`card-${card.title}`">
    <CardHeader :class="nested ? 'px-4.5' : ''">
      <CardTitle>{{ card.title }}</CardTitle>
    </CardHeader>
    <CardContent :class="nested ? 'px-4.5' : ''">
      <SetupRows :rows="card.rows" v-bind="shared" @set="forward" />
      <template v-if="showCards">
        <SetupCard
          v-for="c in card.cards"
          :key="c.title"
          :card="c"
          nested
          v-bind="shared"
          @set="forward"
        />
      </template>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import SetupRows from '@/components/setup/menu/SetupRows.vue';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { CardDef, FieldDef, Read, Row } from '@/lib/setupMenu';
import { isDetails, isField, isGrid } from '@/lib/setupMenu';
import type { Setting } from '@/lib/setupState';

defineOptions({ name: 'SetupCard' });

const props = defineProps<{
  card: CardDef;
  nested?: boolean;
  read: Read;
  settings: Record<string, Setting>;
  errors: Record<string, string>;
  secrets: Record<string, boolean>;
  only?: string[];
}>();

const emit = defineEmits<{ (e: 'set', path: string, value: unknown): void }>();

const shared = computed(() => ({
  read: props.read, settings: props.settings, errors: props.errors, secrets: props.secrets, only: props.only,
}));

const showCards = computed(() => (props.card.showCards ? props.card.showCards(props.read) : true));

const fieldsOf = (rows: Row[]): FieldDef[] => rows.flatMap((r) => {
  if (isGrid(r)) {
    return r.grid;
  }
  if (isDetails(r)) {
    return fieldsOf(r.rows);
  }
  return isField(r) ? [r] : [];
});

const shown = (f: FieldDef) => (!props.only || props.only.includes(f.path)) && (!f.show || f.show(props.read));

const hasContent = computed(() => {
  const all = (c: CardDef): FieldDef[] => [...fieldsOf(c.rows), ...(c.cards ?? []).flatMap(all)];
  return all(props.card).some(shown);
});

const forward = (path: string, value: unknown) => emit('set', path, value);
</script>
