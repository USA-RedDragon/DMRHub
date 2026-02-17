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
    <!-- Loading -->
    <div v-if="loading" class="space-y-6">
      <Card>
        <CardHeader><Skeleton class="h-8 w-64" /></CardHeader>
        <CardContent>
          <div class="grid gap-4 md:grid-cols-2">
            <Skeleton v-for="n in 4" :key="n" class="h-16" />
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Error -->
    <Card v-else-if="error">
      <CardHeader><CardTitle>Error</CardTitle></CardHeader>
      <CardContent><p class="text-destructive">{{ error }}</p></CardContent>
    </Card>

    <!-- Net Details -->
    <template v-else-if="net">
      <Card>
        <CardHeader>
          <div class="flex items-center justify-between">
            <div>
              <CardTitle>
                Net on TG {{ net.talkgroup_id }} — {{ net.talkgroup.name }}
              </CardTitle>
              <CardDescription v-if="net.description">{{ net.description }}</CardDescription>
            </div>
            <div class="flex gap-2">
              <ShadButton v-if="net.active && canControl" variant="destructive" @click="stopCurrentNet">
                Stop Net
              </ShadButton>
              <ShadButton variant="outline" @click="exportCSV">Export CSV</ShadButton>
              <ShadButton variant="outline" @click="exportJSON">Export JSON</ShadButton>
            </div>
          </div>
        </CardHeader>
      </Card>

      <div class="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader><CardTitle>Net Information</CardTitle></CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div class="detail-row">
                <dt class="detail-label">Status</dt>
                <dd class="detail-value">
                  <span v-if="net.active" class="text-green-600 font-semibold">Active</span>
                  <span v-else>Ended</span>
                </dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Started By</dt>
                <dd class="detail-value">{{ net.started_by_user.callsign }} ({{ net.started_by_user.id }})</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Start Time</dt>
                <dd class="detail-value"><RelativeTimestamp :time="net.start_time" /></dd>
              </div>
              <div v-if="net.end_time" class="detail-row">
                <dt class="detail-label">End Time</dt>
                <dd class="detail-value"><RelativeTimestamp :time="net.end_time" /></dd>
              </div>
              <div v-if="net.duration_minutes" class="detail-row">
                <dt class="detail-label">Duration Limit</dt>
                <dd class="detail-value">{{ net.duration_minutes }} minutes</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Check-Ins</dt>
                <dd class="detail-value">{{ net.check_in_count }}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>

      <!-- Check-ins table -->
      <Card>
        <CardHeader>
          <CardTitle>Check-Ins</CardTitle>
          <CardDescription>Calls during this net session</CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable
            :columns="checkInColumns"
            :data="checkIns"
            :loading="checkInsLoading"
            :loading-text="'Loading check-ins...'"
            :empty-text="'No check-ins yet'"
            :manual-pagination="true"
            :page-index="page - 1"
            :page-size="rows"
            :page-count="totalPages"
            @update:page-index="handlePageIndexUpdate"
            @update:page-size="handlePageSizeUpdate"
          />
        </CardContent>
      </Card>
    </template>
  </div>
</template>

<script lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { h } from 'vue';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { DataTable } from '@/components/ui/data-table';
import { Button as ShadButton } from '@/components/ui/button';
import User from '@/components/User.vue';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import { useUserStore } from '@/store';
import {
  getNet,
  getNetCheckIns,
  exportNetCheckIns,
  stopNet,
  type Net,
  type NetCheckIn,
} from '@/services/net';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

export default {
  components: {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    Skeleton,
    DataTable,
    ShadButton,
    RelativeTimestamp,
  },
  head: {
    title: 'Net Details',
  },
  mounted() {
    const id = this.$route.params.id;
    if (id) {
      this.netID = Number(id);
      this.fetchNet();
      this.fetchCheckIns();
      // Connect with net_id param to receive both lifecycle events and live check-ins.
      this.socket = ws.connect(
        getWebsocketURI() + '/nets?net_id=' + this.netID,
        this.onWebsocketMessage,
      );
    }
  },
  unmounted() {
    if (this.socket) {
      this.socket.close();
    }
  },
  data() {
    return {
      netID: 0,
      net: null as Net | null,
      loading: true,
      error: '',
      checkIns: [] as NetCheckIn[],
      checkInsLoading: false,
      totalRecords: 0,
      page: 1,
      rows: 30,
      socket: null as { close(): void } | null,
    };
  },
  computed: {
    canControl(): boolean {
      const userStore = useUserStore();
      return userStore.loggedIn;
    },
    checkInColumns() {
      return [
        {
          accessorKey: 'user',
          header: 'User',
          cell: ({ row }: { row: { original: NetCheckIn } }) =>
            h(User, { user: row.original.user }),
        },
        {
          accessorKey: 'start_time',
          header: 'Time',
          cell: ({ row }: { row: { original: NetCheckIn } }) =>
            h(RelativeTimestamp, { time: row.original.start_time }),
        },
        {
          accessorKey: 'duration',
          header: 'Duration',
          cell: ({ row }: { row: { original: NetCheckIn } }) =>
            `${(row.original.duration / 1000000000).toFixed(1)}s`,
        },
        {
          accessorKey: 'time_slot',
          header: 'TS',
          cell: ({ row }: { row: { original: NetCheckIn } }) =>
            row.original.time_slot ? '2' : '1',
        },
        {
          accessorKey: 'ber',
          header: 'BER',
          cell: ({ row }: { row: { original: NetCheckIn } }) =>
            `${(row.original.ber * 100).toFixed(1)}%`,
        },
        {
          accessorKey: 'loss',
          header: 'Loss',
          cell: ({ row }: { row: { original: NetCheckIn } }) =>
            `${(row.original.loss * 100).toFixed(1)}%`,
        },
        {
          accessorKey: 'rssi',
          header: 'RSSI',
          cell: ({ row }: { row: { original: NetCheckIn } }) => {
            const rssi = Number(row.original.rssi);
            return rssi !== 0 ? `-${rssi}dBm` : '—';
          },
        },
      ] as ColumnDef<NetCheckIn, unknown>[];
    },
    totalPages(): number {
      if (!this.totalRecords || this.totalRecords <= 0) return 1;
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
  },
  methods: {
    fetchNet() {
      this.loading = true;
      getNet(this.netID)
        .then((res) => {
          this.net = res.data;
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.error = 'Failed to load net details.';
          this.loading = false;
        });
    },
    fetchCheckIns(page = 1, limit = 30) {
      this.checkInsLoading = true;
      getNetCheckIns(this.netID, { page, limit })
        .then((res) => {
          this.totalRecords = res.data.total;
          this.checkIns = res.data.check_ins || [];
          this.checkInsLoading = false;
        })
        .catch((err) => {
          console.error(err);
          this.checkInsLoading = false;
        });
    },
    stopCurrentNet() {
      if (!this.net) return;
      stopNet(this.net.id)
        .then((res) => {
          this.net = res.data;
        })
        .catch((err) => {
          console.error(err);
        });
    },
    exportCSV() {
      exportNetCheckIns(this.netID, 'csv')
        .then((res) => {
          const blob = new Blob([res.data], { type: 'text/csv' });
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url;
          a.download = `net_${this.netID}_checkins.csv`;
          a.click();
          URL.revokeObjectURL(url);
        })
        .catch((err) => console.error(err));
    },
    exportJSON() {
      exportNetCheckIns(this.netID, 'json')
        .then((res) => {
          const blob = new Blob([JSON.stringify(res.data, null, 2)], {
            type: 'application/json',
          });
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url;
          a.download = `net_${this.netID}_checkins.json`;
          a.click();
          URL.revokeObjectURL(url);
        })
        .catch((err) => console.error(err));
    },
    handlePageIndexUpdate(pageIndex: number) {
      const nextPage = pageIndex + 1;
      if (nextPage === this.page || nextPage < 1) return;
      this.page = nextPage;
      this.fetchCheckIns(this.page, this.rows);
    },
    handlePageSizeUpdate(pageSize: number) {
      if (pageSize === this.rows || pageSize <= 0) return;
      this.rows = pageSize;
      this.page = 1;
      this.fetchCheckIns(this.page, this.rows);
    },
    onWebsocketMessage(event: MessageEvent) {
      const data = JSON.parse(event.data);

      // Check-in event (from net:checkins:{id} topic)
      if (data.call_id && data.user) {
        // Prepend the new check-in to the live list.
        const newCheckIn: NetCheckIn = {
          call_id: data.call_id,
          user: data.user,
          start_time: data.start_time,
          duration: data.duration,
          time_slot: false,
          loss: 0,
          jitter: 0,
          ber: 0,
          rssi: 0,
        };
        // Avoid duplicates.
        const exists = this.checkIns.some((ci) => ci.call_id === newCheckIn.call_id);
        if (!exists) {
          this.checkIns = [newCheckIn, ...this.checkIns];
          this.totalRecords++;
        }
        // Update the check-in count on the net.
        if (this.net) {
          this.net.check_in_count = this.totalRecords;
        }
        return;
      }

      // Net lifecycle event (from net:events topic)
      if (data.net_id === this.netID) {
        if (data.event === 'stopped') {
          this.fetchNet();
        }
      }
    },
  },
};
</script>

<style scoped>
.detail-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.detail-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 1rem;
}

.detail-label {
  font-size: 0.875rem;
  color: hsl(var(--muted-foreground));
  flex-shrink: 0;
}

.detail-value {
  font-size: 0.875rem;
  font-weight: 500;
  text-align: right;
}
</style>
