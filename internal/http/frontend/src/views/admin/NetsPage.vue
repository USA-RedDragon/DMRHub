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
  <div>
    <h1>Nets</h1>
    <DataTable
      :columns="columns"
      :data="nets"
      :loading="loading"
      :loading-text="'Loading nets...'"
      :empty-text="'No nets found'"
      :manual-pagination="true"
      :page-index="page - 1"
      :page-size="rows"
      :page-count="totalPages"
      @update:page-index="handlePageIndexUpdate"
      @update:page-size="handlePageSizeUpdate"
    />

    <h2 class="mt-8 mb-4">Scheduled Nets</h2>
    <DataTable
      :columns="scheduledColumns"
      :data="scheduledNets"
      :loading="scheduledLoading"
      :loading-text="'Loading scheduled nets...'"
      :empty-text="'No scheduled nets'"
      :manual-pagination="true"
      :page-index="scheduledPage - 1"
      :page-size="scheduledRows"
      :page-count="scheduledTotalPages"
      @update:page-index="handleScheduledPageIndexUpdate"
      @update:page-size="handleScheduledPageSizeUpdate"
    />
  </div>
</template>

<script lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { h } from 'vue';
import { RouterLink } from 'vue-router';
import { DataTable } from '@/components/ui/data-table';
import { Button as ShadButton } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import {
  getNets,
  getScheduledNets,
  patchNet,
  updateScheduledNet,
  deleteScheduledNet,
  type Net,
  type ScheduledNet,
} from '@/services/net';

const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export default {
  components: {
    DataTable,
  },
  head: {
    title: 'Nets',
    titleTemplate: 'Admin | %s | ' + (localStorage.getItem('title') || 'DMRHub'),
  },
  mounted() {
    this.fetchNets();
    this.fetchScheduledNets();
  },
  data() {
    return {
      nets: [] as Net[],
      loading: false,
      totalRecords: 0,
      page: 1,
      rows: 30,
      scheduledNets: [] as ScheduledNet[],
      scheduledLoading: false,
      scheduledTotalRecords: 0,
      scheduledPage: 1,
      scheduledRows: 30,
    };
  },
  computed: {
    columns() {
      return [
        {
          accessorKey: 'id',
          header: 'ID',
          cell: ({ row }: { row: { original: Net } }) =>
            h(
              RouterLink,
              { to: `/nets/${row.original.id}`, class: 'text-primary hover:underline' },
              () => String(row.original.id),
            ),
        },
        {
          accessorKey: 'talkgroup',
          header: 'Talkgroup',
          cell: ({ row }: { row: { original: Net } }) =>
            h(
              RouterLink,
              { to: `/talkgroups/${row.original.talkgroup_id}` },
              () => `TG ${row.original.talkgroup_id} — ${row.original.talkgroup.name}`,
            ),
        },
        {
          accessorKey: 'active',
          header: 'Status',
          cell: ({ row }: { row: { original: Net } }) =>
            row.original.active
              ? h('span', { class: 'text-green-600 font-semibold' }, 'Active')
              : 'Ended',
        },
        {
          accessorKey: 'start_time',
          header: 'Started',
          cell: ({ row }: { row: { original: Net } }) =>
            h(RelativeTimestamp, { time: row.original.start_time }),
        },
        {
          accessorKey: 'check_in_count',
          header: 'Check-Ins',
          cell: ({ row }: { row: { original: Net } }) => String(row.original.check_in_count),
        },
        {
          accessorKey: 'showcase',
          header: 'Showcase',
          cell: ({ row }: { row: { original: Net } }) => {
            const net = row.original;
            return h(Switch, {
              modelValue: net.showcase,
              'onUpdate:modelValue': (val: boolean) => this.toggleShowcase(net.id, val),
            });
          },
        },
      ] as ColumnDef<Net, unknown>[];
    },
    scheduledColumns() {
      return [
        {
          accessorKey: 'id',
          header: 'ID',
          cell: ({ row }: { row: { original: ScheduledNet } }) => String(row.original.id),
        },
        {
          accessorKey: 'name',
          header: 'Name',
          cell: ({ row }: { row: { original: ScheduledNet } }) => row.original.name,
        },
        {
          accessorKey: 'talkgroup',
          header: 'Talkgroup',
          cell: ({ row }: { row: { original: ScheduledNet } }) =>
            h(
              RouterLink,
              { to: `/talkgroups/${row.original.talkgroup_id}` },
              () => `TG ${row.original.talkgroup_id} — ${row.original.talkgroup.name}`,
            ),
        },
        {
          accessorKey: 'schedule',
          header: 'Schedule',
          cell: ({ row }: { row: { original: ScheduledNet } }) => {
            const sn = row.original;
            return `${dayNames[sn.day_of_week]} ${sn.time_of_day} ${sn.timezone}`;
          },
        },
        {
          accessorKey: 'enabled',
          header: 'Enabled',
          cell: ({ row }: { row: { original: ScheduledNet } }) =>
            row.original.enabled
              ? h('span', { class: 'text-green-600' }, 'Yes')
              : h('span', { class: 'text-muted-foreground' }, 'No'),
        },
        {
          accessorKey: 'showcase',
          header: 'Showcase',
          cell: ({ row }: { row: { original: ScheduledNet } }) => {
            const sn = row.original;
            return h(Switch, {
              modelValue: sn.showcase,
              'onUpdate:modelValue': (val: boolean) => this.toggleScheduledShowcase(sn.id, val),
            });
          },
        },
        {
          accessorKey: 'actions',
          header: '',
          cell: ({ row }: { row: { original: ScheduledNet } }) =>
            h('div', { class: 'flex gap-2' }, [
              h(
                RouterLink,
                {
                  to: `/nets/scheduled/${row.original.id}/edit`,
                  class: 'text-primary hover:underline text-sm',
                },
                () => h(ShadButton, { variant: 'outline', size: 'sm' }, () => 'Edit')
              ),
              h(
                ShadButton,
                {
                  variant: 'outline',
                  size: 'sm',
                  class: 'text-destructive',
                  onClick: () => this.handleDeleteScheduledNet(row.original.id),
                },
                () => 'Delete',
              ),
            ]),
        },
      ] as ColumnDef<ScheduledNet, unknown>[];
    },
    totalPages(): number {
      if (!this.totalRecords || this.totalRecords <= 0) return 1;
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
    scheduledTotalPages(): number {
      if (!this.scheduledTotalRecords || this.scheduledTotalRecords <= 0) return 1;
      return Math.max(1, Math.ceil(this.scheduledTotalRecords / this.scheduledRows));
    },
  },
  methods: {
    fetchNets(page = 1, limit = 30) {
      this.loading = true;
      getNets({ page, limit })
        .then((res) => {
          this.totalRecords = res.data.total;
          this.nets = res.data.nets || [];
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.loading = false;
        });
    },
    fetchScheduledNets(page = 1, limit = 30) {
      this.scheduledLoading = true;
      getScheduledNets({ page, limit })
        .then((res) => {
          this.scheduledTotalRecords = res.data.total;
          this.scheduledNets = res.data.scheduled_nets || [];
          this.scheduledLoading = false;
        })
        .catch((err) => {
          console.error(err);
          this.scheduledLoading = false;
        });
    },
    toggleShowcase(netID: number, showcase: boolean) {
      patchNet(netID, { showcase })
        .then((res) => {
          this.nets = this.nets.map((n: Net) =>
            n.id === netID
              ? { ...n, showcase: res.data?.showcase ?? showcase }
              : n,
          );
        })
        .catch((err) => console.error(err));
    },
    toggleScheduledShowcase(snID: number, showcase: boolean) {
      updateScheduledNet(snID, { showcase })
        .then((res) => {
          this.scheduledNets = this.scheduledNets.map((sn: ScheduledNet) =>
            sn.id === snID
              ? { ...sn, showcase: res.data?.showcase ?? showcase }
              : sn,
          );
        })
        .catch((err) => console.error(err));
    },
    handleDeleteScheduledNet(id: number) {
      if (!confirm('Are you sure you want to delete this scheduled net?')) return;
      deleteScheduledNet(id)
        .then(() => {
          this.fetchScheduledNets(this.scheduledPage, this.scheduledRows);
        })
        .catch((err) => console.error(err));
    },
    handlePageIndexUpdate(pageIndex: number) {
      const nextPage = pageIndex + 1;
      if (nextPage === this.page || nextPage < 1) return;
      this.page = nextPage;
      this.fetchNets(this.page, this.rows);
    },
    handlePageSizeUpdate(pageSize: number) {
      if (pageSize === this.rows || pageSize <= 0) return;
      this.rows = pageSize;
      this.page = 1;
      this.fetchNets(this.page, this.rows);
    },
    handleScheduledPageIndexUpdate(pageIndex: number) {
      const nextPage = pageIndex + 1;
      if (nextPage === this.scheduledPage || nextPage < 1) return;
      this.scheduledPage = nextPage;
      this.fetchScheduledNets(this.scheduledPage, this.scheduledRows);
    },
    handleScheduledPageSizeUpdate(pageSize: number) {
      if (pageSize === this.scheduledRows || pageSize <= 0) return;
      this.scheduledRows = pageSize;
      this.scheduledPage = 1;
      this.fetchScheduledNets(this.scheduledPage, this.scheduledRows);
    },
  },
};
</script>
