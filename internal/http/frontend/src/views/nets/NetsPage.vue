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
  <div class="space-y-6">
    <!-- Showcase nets -->
    <div v-if="showcaseNets.length > 0" class="space-y-3">
      <h2 class="text-lg font-semibold">Featured Nets</h2>
      <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <Card
          v-for="net in showcaseNets"
          :key="net.id"
          :class="net.active
            ? 'border-green-500/30 bg-green-50/5'
            : 'border-muted bg-muted/5'"
        >
          <CardHeader class="pb-2">
            <div class="flex items-center gap-2">
              <span
                v-if="net.active"
                class="inline-block h-2 w-2 rounded-full bg-green-500 animate-pulse"
              />
              <CardTitle class="text-base">
                <RouterLink :to="`/talkgroups/${net.talkgroup_id}`" class="hover:underline">
                  TG {{ net.talkgroup_id }} — {{ net.talkgroup.name }}
                </RouterLink>
              </CardTitle>
            </div>
            <CardDescription v-if="net.description">{{ net.description }}</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="flex items-center justify-between text-sm">
              <span class="text-muted-foreground">
                Started by {{ net.started_by_user.callsign }}
              </span>
              <span class="font-medium">{{ net.check_in_count }} check-ins</span>
            </div>
            <div class="mt-2">
              <RouterLink
                :to="`/nets/${net.id}`"
                class="text-sm text-primary hover:underline"
              >
                View Details
              </RouterLink>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>

    <!-- Upcoming scheduled nets -->
    <div v-if="upcomingNets.length > 0" class="space-y-3">
      <h2 class="text-lg font-semibold">Upcoming Nets</h2>
      <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <Card v-for="sn in upcomingNets" :key="sn.id" class="border-blue-500/30 bg-blue-50/5">
          <CardHeader class="pb-2">
            <div class="flex items-center gap-2">
              <span class="inline-block h-2 w-2 rounded-full bg-blue-500" />
              <CardTitle class="text-base">
                <RouterLink :to="`/talkgroups/${sn.talkgroup_id}`" class="hover:underline">
                  TG {{ sn.talkgroup_id }} — {{ sn.talkgroup.name }}
                </RouterLink>
              </CardTitle>
            </div>
            <CardDescription v-if="sn.description">{{ sn.description }}</CardDescription>
          </CardHeader>
          <CardContent>
            <div class="flex items-center justify-between text-sm">
              <span class="text-muted-foreground">{{ sn.name }}</span>
              <span v-if="sn.next_run" class="font-medium">
                {{ formatNextRun(sn.next_run) }}
              </span>
            </div>
            <div class="mt-1 text-xs text-muted-foreground">
              {{ dayNames[sn.day_of_week] }} at {{ sn.time_of_day }} {{ sn.timezone }}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>

    <!-- All nets table -->
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div>
            <CardTitle>Nets</CardTitle>
            <CardDescription>Active and past net check-in sessions</CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
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
      </CardContent>
    </Card>
  </div>
</template>

<script lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { h } from 'vue';
import { RouterLink } from 'vue-router';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { DataTable } from '@/components/ui/data-table';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import { getNets, getScheduledNets, type Net, type ScheduledNet } from '@/services/net';
import { getWebsocketURI } from '@/services/util';
import { formatDistanceToNow, isPast } from 'date-fns';
import ws from '@/services/ws';

const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export default {
  components: {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    DataTable,
  },
  head: {
    title: 'Nets',
  },
  mounted() {
    this.fetchNets();
    this.fetchShowcaseNets();
    this.fetchUpcomingNets();
    this.socket = ws.connect(getWebsocketURI() + '/nets', this.onWebsocketMessage);
  },
  unmounted() {
    if (this.socket) {
      this.socket.close();
    }
  },
  data() {
    return {
      nets: [] as Net[],
      showcaseNets: [] as Net[],
      upcomingNets: [] as ScheduledNet[],
      loading: false,
      totalRecords: 0,
      page: 1,
      rows: 30,
      socket: null as { close(): void } | null,
      dayNames,
    };
  },
  computed: {
    columns() {
      return [
        {
          accessorKey: 'talkgroup',
          header: 'Talkgroup',
          cell: ({ row }: { row: { original: Net } }) => {
            const net = row.original;
            return h(
              RouterLink,
              { to: `/talkgroups/${net.talkgroup_id}` },
              () => `TG ${net.talkgroup_id} — ${net.talkgroup.name}`,
            );
          },
        },
        {
          accessorKey: 'description',
          header: 'Description',
          cell: ({ row }: { row: { original: Net } }) => row.original.description || '—',
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
          accessorKey: 'actions',
          header: '',
          cell: ({ row }: { row: { original: Net } }) => {
            return h(
              RouterLink,
              { to: `/nets/${row.original.id}`, class: 'text-primary hover:underline' },
              () => 'View',
            );
          },
        },
      ] as ColumnDef<Net, unknown>[];
    },
    totalPages(): number {
      if (!this.totalRecords || this.totalRecords <= 0) return 1;
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
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
    onWebsocketMessage(event: MessageEvent) {
      const data = JSON.parse(event.data);
      if (data.event === 'started' || data.event === 'stopped') {
        // Refresh both lists when a net starts or stops.
        this.fetchNets(this.page, this.rows);
        this.fetchShowcaseNets();
      }
    },
    fetchShowcaseNets() {
      getNets({ showcase: true })
        .then((res) => {
          this.showcaseNets = res.data.nets || [];
        })
        .catch((err) => console.error(err));
    },
    fetchUpcomingNets() {
      getScheduledNets({ limit: 30 })
        .then((res) => {
          const all = res.data.scheduled_nets || [];
          // Show enabled scheduled nets with a future next_run.
          this.upcomingNets = all.filter(
            (sn) => sn.enabled && sn.next_run && !isPast(new Date(sn.next_run)),
          );
        })
        .catch((err) => console.error(err));
    },
    formatNextRun(nextRun: string): string {
      return formatDistanceToNow(new Date(nextRun), { addSuffix: true });
    },
  },
};
</script>
