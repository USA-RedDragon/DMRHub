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
  <DataTable
    :columns="columns"
    :data="messages"
    :loading="loading"
    loading-text="Loading..."
    :empty-text="emptyText"
    :manual-pagination="true"
    :page-index="page - 1"
    :page-size="pageSize"
    :page-count="pageCount"
    :summary="summary"
    @update:page-index="(index: number) => emit('update:page', index + 1)"
  />
</template>

<script setup lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { computed, h, type VNode } from 'vue';
import { DataTable } from '@/components/ui/data-table';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import Talkgroup from '@/components/Talkgroup.vue';
import User from '@/components/User.vue';
import { badgeClass } from '@/lib/badge';
import { formatLabel, messageState, notTextLabel, type Message } from '@/services/messages';

withDefaults(
  defineProps<{
    messages: Message[];
    page: number;
    pageSize: number;
    pageCount: number;
    loading?: boolean;
    emptyText?: string;
    summary?: string;
  }>(),
  {
    loading: false,
    emptyText: 'No messages yet',
    summary: '',
  },
);

const emit = defineEmits<{
  (e: 'update:page', value: number): void
}>();

type Cell = { row: { original: Message } };

const party = (id: number, callsign: string): VNode => {
  if (callsign) {
    return h(User, { user: { id, callsign } });
  }
  return h('span', { class: 'font-medium tabular-nums' }, String(id));
};

const destination = (m: Message): VNode => {
  if (m.group) {
    return h('div', { class: 'inline-flex items-center gap-2' }, [
      h(Talkgroup, { talkgroup: { id: m.destination_id } }),
      h('span', { class: badgeClass('none') }, 'Group'),
    ]);
  }
  return party(m.destination_id, m.destination_callsign);
};

const textCell = (m: Message): VNode => {
  const parts: VNode[] = [];
  if (m.text === null) {
    parts.push(h('span', { class: 'text-muted-foreground' }, notTextLabel(m)));
  } else {
    parts.push(h('span', m.text));
  }
  if (!m.checksum_ok) {
    parts.push(h('span', { class: `ml-1 ${badgeClass('warn')}` }, 'Checksum bad'));
  }
  return h('div', { class: 'min-w-48 max-w-md whitespace-normal [overflow-wrap:anywhere]' }, parts);
};

const columns = computed(() => [
  {
    id: 'time',
    header: 'Time',
    cell: ({ row }: Cell) => h(RelativeTimestamp, { time: row.original.received_at }),
  },
  {
    id: 'from',
    header: 'From',
    cell: ({ row }: Cell) => party(row.original.source_id, row.original.source_callsign),
  },
  {
    id: 'to',
    header: 'To',
    cell: ({ row }: Cell) => destination(row.original),
  },
  {
    id: 'format',
    header: 'Format',
    cell: ({ row }: Cell) => h('span', { class: badgeClass('info') }, formatLabel(row.original.format)),
  },
  {
    id: 'text',
    header: 'Text',
    cell: ({ row }: Cell) => textCell(row.original),
  },
  {
    id: 'state',
    header: 'State',
    cell: ({ row }: Cell) => {
      const state = messageState(row.original);
      return h('span', { class: badgeClass(state.tone) }, state.label);
    },
  },
] as ColumnDef<Message, unknown>[]);
</script>
