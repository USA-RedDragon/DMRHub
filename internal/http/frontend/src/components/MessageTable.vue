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
    :row-clickable="admin"
    :row-selected="(m: Message) => m.id === selectedId"
    @update:page-index="(index: number) => emit('update:page', index + 1)"
    @row-click="(m: Message) => emit('select', m.id)"
  />
</template>

<script setup lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { computed, h, type VNode } from 'vue';
import { RouterLink } from 'vue-router';
import { DataTable } from '@/components/ui/data-table';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import Talkgroup from '@/components/Talkgroup.vue';
import User from '@/components/User.vue';
import { badgeClass } from '@/lib/badge';
import {
  formatLabel,
  messageState,
  notTextLabel,
  transportLabel,
  type AdminMessage,
  type Message,
} from '@/services/messages';

const props = withDefaults(
  defineProps<{
    messages: Message[];
    page: number;
    pageSize: number;
    pageCount: number;
    loading?: boolean;
    emptyText?: string;
    summary?: string;
    admin?: boolean;
    selectedId?: number | null;
  }>(),
  {
    loading: false,
    emptyText: 'No messages yet',
    summary: '',
    admin: false,
    selectedId: null,
  },
);

const emit = defineEmits<{
  (e: 'update:page', value: number): void
  (e: 'select', value: number): void
}>();

const shortTime = (value: string) =>
  new Date(value).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });

const longTime = (value: string) =>
  new Date(value).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short',
  });

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

const timeCell = (m: Message): VNode => {
  if (props.admin) {
    return h('span', { title: longTime(m.received_at) }, shortTime(m.received_at));
  }
  return h(RelativeTimestamp, { time: m.received_at });
};

const repeaterLink = (id: number): VNode =>
  h(
    RouterLink,
    {
      to: `/repeaters/${id}`,
      class: 'text-primary underline font-medium',
    },
    () => String(id),
  );

const userColumns: ColumnDef<Message, unknown>[] = [
  {
    id: 'time',
    header: 'Time',
    cell: ({ row }: Cell) => timeCell(row.original),
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
];

const adminColumns: ColumnDef<Message, unknown>[] = [
  {
    id: 'repeater',
    header: 'Repeater',
    cell: ({ row }: Cell) => repeaterLink((row.original as AdminMessage).repeater_id),
  },
  {
    id: 'slot',
    header: 'TS',
    cell: ({ row }: Cell) => String((row.original as AdminMessage).slot),
  },
  {
    id: 'network',
    header: 'Network',
    cell: ({ row }: Cell) => transportLabel((row.original as AdminMessage).transport),
  },
];

const columns = computed(() => (props.admin ? [...userColumns, ...adminColumns] : userColumns));
</script>
