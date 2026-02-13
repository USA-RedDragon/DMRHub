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
    :data="lastheard"
    :loading="loading"
    :loading-text="'Loading...'"
    :empty-text="'No calls found'"
    :manual-pagination="true"
    :page-index="page - 1"
    :page-size="rows"
    :page-count="totalPages"
    @update:page-index="handlePageIndexUpdate"
    @update:page-size="handlePageSizeUpdate"
  />
</template>

<script lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { h } from 'vue';
import { DataTable } from '@/components/ui/data-table';

import { format, formatDistanceToNowStrict } from 'date-fns';

import { getWebsocketURI } from '@/services/util';
import API from '@/services/API';
import ws from '@/services/ws';

type LastHeardRow = {
  id: number;
  active: boolean;
  start_time: string | Date;
  time_slot: boolean;
  user: { id: number; callsign: string };
  is_to_talkgroup: boolean;
  to_talkgroup: { id: number };
  is_to_repeater: boolean;
  to_repeater: { id: number; callsign: string };
  is_to_user: boolean;
  to_user: { id: number; callsign: string };
  duration: string | number;
  ber: string | number;
  loss: string | number;
  jitter: string | number;
  rssi: number;
};

export default {
  name: 'LastHeardTable',
  props: {},
  components: {
    DataTable,
  },
  data: function() {
    return {
      lastheard: [] as LastHeardRow[],
      totalRecords: 0,
      page: 1,
      rows: 30,
      socket: null as { close(): void } | null,
      loading: false,
    };
  },
  mounted() {
    this.fetchData();
    this.socket = ws.connect(getWebsocketURI() + '/calls', this.onWebsocketMessage);
  },
  unmounted() {
    if (this.socket) {
      this.socket.close();
    }
  },
  computed: {
    columns() {
      return [
        {
          accessorKey: 'time',
          header: 'Time',
          cell: ({ row }: { row: { original: LastHeardRow } }) => {
            const call = row.original;
            if (call.active) {
              return 'Active';
            }
            return h('span', { title: this.absoluteTime(call.start_time) }, this.relativeTime(call.start_time));
          },
        },
        {
          accessorKey: 'time_slot',
          header: 'TS',
          cell: ({ row }: { row: { original: LastHeardRow } }) => (row.original.time_slot ? '2' : '1'),
        },
        {
          accessorKey: 'user',
          header: 'User',
          cell: ({ row }: { row: { original: LastHeardRow } }) => {
            const call = row.original;
            return `${call.user.callsign} | ${call.user.id}`;
          },
        },
        {
          accessorKey: 'destination',
          header: 'Destination',
          cell: ({ row }: { row: { original: LastHeardRow } }) => {
            const call = row.original;
            if (call.is_to_talkgroup) {
              return `TG ${call.to_talkgroup.id}`;
            }
            if (call.is_to_repeater) {
              return `${call.to_repeater.callsign} | ${call.to_repeater.id}`;
            }
            if (call.is_to_user) {
              return `${call.to_user.callsign} | ${call.to_user.id}`;
            }
            return '-';
          },
        },
        {
          accessorKey: 'duration',
          header: 'Duration',
          cell: ({ row }: { row: { original: LastHeardRow } }) => `${row.original.duration}s`,
        },
        {
          accessorKey: 'ber',
          header: 'BER',
          cell: ({ row }: { row: { original: LastHeardRow } }) => `${row.original.ber}%`,
        },
        {
          accessorKey: 'loss',
          header: 'Loss',
          cell: ({ row }: { row: { original: LastHeardRow } }) => `${row.original.loss}%`,
        },
        {
          accessorKey: 'jitter',
          header: 'Jitter',
          cell: ({ row }: { row: { original: LastHeardRow } }) => `${row.original.jitter}ms`,
        },
        {
          accessorKey: 'rssi',
          header: 'RSSI',
          cell: ({ row }: { row: { original: LastHeardRow } }) => {
            const rssi = Number(row.original.rssi);
            return rssi !== 0 ? `-${rssi}dBm` : '-';
          },
        },
      ] as ColumnDef<LastHeardRow, unknown>[];
    },
    totalPages() {
      if (!this.totalRecords || this.totalRecords <= 0) {
        return 1;
      }
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
  },
  methods: {
    handlePageIndexUpdate(pageIndex: number) {
      const nextPage = pageIndex + 1;
      if (nextPage === this.page || nextPage < 1) {
        return;
      }
      this.page = nextPage;
      this.fetchData(this.page, this.rows);
    },
    handlePageSizeUpdate(pageSize: number) {
      if (pageSize === this.rows || pageSize <= 0) {
        return;
      }
      this.rows = pageSize;
      this.page = 1;
      this.fetchData(this.page, this.rows);
    },
    fetchData(page = 1, limit = 30) {
      this.loading = true;
      API.get(`/lastheard?page=${page}&limit=${limit}`)
        .then((res) => {
          this.totalRecords = res.data.total;
          this.lastheard = this.cleanData(res.data.calls);
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.loading = false;
        });
    },
    cleanData(data: Record<string, unknown>[]) {
      const copyData = JSON.parse(JSON.stringify(data));
      for (let i = 0; i < copyData.length; i++) {
        copyData[i].start_time = new Date(copyData[i].start_time);

        if (typeof copyData[i].duration == 'number') {
          copyData[i].duration = (copyData[i].duration / 1000000000).toFixed(1);
        }

        // loss is in a ratio, multiply by 100 to get a percentage
        if (typeof copyData[i].loss == 'number') {
          copyData[i].loss = (copyData[i].loss * 100).toFixed(1);
        }

        if (typeof copyData[i].ber == 'number') {
          copyData[i].ber = (copyData[i].ber * 100).toFixed(1);
        }

        if (typeof copyData[i].jitter == 'number') {
          copyData[i].jitter = copyData[i].jitter.toFixed(1);
        }

        if (typeof copyData[i].rssi == 'number') {
          copyData[i].rssi = copyData[i].rssi.toFixed(0);
        }
      }

      return copyData;
    },
    relativeTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return formatDistanceToNowStrict(date, { addSuffix: true });
    },
    absoluteTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return format(date, 'yyyy-MM-dd HH:mm:ss');
    },
    onWebsocketMessage(event: MessageEvent) {
      const call = JSON.parse(event.data);
      // We need to check that the call is not already in the table
      // If it is, we need to update it
      // If it isn't, we need to add it
      let found = false;
      const copyLastheard = JSON.parse(JSON.stringify(this.lastheard));

      for (let i = 0; i < copyLastheard.length; i++) {
        if (copyLastheard[i].id == call.id) {
          found = true;
          copyLastheard[i] = call;
          break;
        }
      }

      if (!found && copyLastheard.length === this.rows) {
        copyLastheard.pop();
      }

      if (!found && copyLastheard.length < this.rows) {
        copyLastheard.unshift(call);
      }

      this.lastheard = this.cleanData(copyLastheard);
    },
  },
};
</script>

<style scoped></style>
