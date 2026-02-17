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
    <!-- Loading skeleton -->
    <div v-if="loading" class="space-y-6">
      <Card>
        <CardHeader>
          <Skeleton class="h-8 w-64" />
        </CardHeader>
        <CardContent>
          <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <Skeleton v-for="n in 6" :key="n" class="h-16" />
          </div>
        </CardContent>
      </Card>
    </div>

    <!-- Error state -->
    <Card v-else-if="error">
      <CardHeader>
        <CardTitle>Error</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-destructive">{{ error }}</p>
      </CardContent>
    </Card>

    <!-- Repeater details -->
    <template v-else-if="repeater">
      <!-- Header card -->
      <Card>
        <CardHeader>
          <div class="flex items-center justify-between">
            <div>
              <CardTitle>{{ repeater.callsign || 'Repeater' }} — {{ repeater.id }}</CardTitle>
              <CardDescription v-if="repeater.description">{{ repeater.description }}</CardDescription>
            </div>
            <div class="flex items-center gap-2">
              <span class="inline-flex items-center rounded-md px-2.5 py-0.5 text-xs font-medium"
                :class="isOnline ? 'bg-green-500/10 text-green-500' : 'bg-muted text-muted-foreground'">
                {{ isOnline ? 'Online' : 'Offline' }}
              </span>
              <span class="inline-flex items-center rounded-md bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
                {{ (repeater.type || 'mmdvm').toUpperCase() }}
              </span>
              <span v-if="repeater.simplex_repeater"
                class="inline-flex items-center rounded-md bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
                Simplex
              </span>
            </div>
          </div>
        </CardHeader>
      </Card>

      <!-- Info grid -->
      <div class="grid gap-6 md:grid-cols-2">
        <!-- Radio Configuration -->
        <Card>
          <CardHeader>
            <CardTitle>Radio Configuration</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div class="detail-row">
                <dt class="detail-label">RX Frequency</dt>
                <dd class="detail-value">{{ formatFrequency(repeater.rx_frequency) }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">TX Frequency</dt>
                <dd class="detail-value">{{ formatFrequency(repeater.tx_frequency) }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">TX Power</dt>
                <dd class="detail-value">{{ repeater.tx_power }}W</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Color Code</dt>
                <dd class="detail-value">{{ repeater.color_code }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Timeslots</dt>
                <dd class="detail-value">{{ repeater.slots }}</dd>
              </div>
              <div v-if="repeater.height" class="detail-row">
                <dt class="detail-label">Height</dt>
                <dd class="detail-value">{{ repeater.height }}m</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <!-- Location & Owner -->
        <Card>
          <CardHeader>
            <CardTitle>Location &amp; Owner</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div v-if="repeater.owner" class="detail-row">
                <dt class="detail-label">Owner</dt>
                <dd class="detail-value">
                  <User :user="repeater.owner" />
                </dd>
              </div>
              <div v-if="repeater.location" class="detail-row">
                <dt class="detail-label">Location</dt>
                <dd class="detail-value">{{ repeater.location }}</dd>
              </div>
              <div v-if="hasCoordinates" class="detail-row">
                <dt class="detail-label">Coordinates</dt>
                <dd class="detail-value">{{ repeater.latitude.toFixed(6) }}, {{ repeater.longitude.toFixed(6) }}</dd>
              </div>
              <div v-if="repeater.url" class="detail-row">
                <dt class="detail-label">URL</dt>
                <dd class="detail-value">
                  <a :href="repeater.url" target="_blank" class="text-primary underline">{{ repeater.url }}</a>
                </dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Created</dt>
                <dd class="detail-value">
                  <RelativeTimestamp :time="repeater.created_at" />
                </dd>
              </div>
              <div v-if="hasTimestamp(repeater.connected_time)" class="detail-row">
                <dt class="detail-label">Last Connected</dt>
                <dd class="detail-value">
                  <RelativeTimestamp :time="repeater.connected_time" />
                </dd>
              </div>
              <div v-if="hasTimestamp(repeater.last_ping_time)" class="detail-row">
                <dt class="detail-label">Last Ping</dt>
                <dd class="detail-value">
                  <RelativeTimestamp :time="repeater.last_ping_time" />
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <!-- Software Info -->
        <Card v-if="repeater.software_id || repeater.package_id">
          <CardHeader>
            <CardTitle>Software</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div v-if="repeater.software_id" class="detail-row">
                <dt class="detail-label">Software ID</dt>
                <dd class="detail-value">{{ repeater.software_id }}</dd>
              </div>
              <div v-if="repeater.package_id" class="detail-row">
                <dt class="detail-label">Package ID</dt>
                <dd class="detail-value">{{ repeater.package_id }}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <!-- Talkgroup Subscriptions -->
        <Card>
          <CardHeader>
            <CardTitle>Talkgroup Subscriptions</CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-4">
              <!-- TS1 -->
              <div>
                <h4 class="text-sm font-semibold mb-2">Timeslot 1</h4>
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="text-xs text-muted-foreground w-16">Static:</span>
                    <div v-if="repeater.ts1_static_talkgroups && repeater.ts1_static_talkgroups.length > 0" class="flex flex-wrap gap-1">
                      <Talkgroup v-for="tg in repeater.ts1_static_talkgroups" :key="tg.id" :talkgroup="tg" />
                    </div>
                    <span v-else class="text-sm text-muted-foreground">None</span>
                  </div>
                  <div class="flex items-center gap-2">
                    <span class="text-xs text-muted-foreground w-16">Dynamic:</span>
                    <Talkgroup v-if="repeater.ts1_dynamic_talkgroup && repeater.ts1_dynamic_talkgroup.id"
                      :talkgroup="repeater.ts1_dynamic_talkgroup" />
                    <span v-else class="text-sm text-muted-foreground">None</span>
                  </div>
                </div>
              </div>
              <Separator />
              <!-- TS2 -->
              <div>
                <h4 class="text-sm font-semibold mb-2">Timeslot 2</h4>
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="text-xs text-muted-foreground w-16">Static:</span>
                    <div v-if="repeater.ts2_static_talkgroups && repeater.ts2_static_talkgroups.length > 0" class="flex flex-wrap gap-1">
                      <Talkgroup v-for="tg in repeater.ts2_static_talkgroups" :key="tg.id" :talkgroup="tg" />
                    </div>
                    <span v-else class="text-sm text-muted-foreground">None</span>
                  </div>
                  <div class="flex items-center gap-2">
                    <span class="text-xs text-muted-foreground w-16">Dynamic:</span>
                    <Talkgroup v-if="repeater.ts2_dynamic_talkgroup && repeater.ts2_dynamic_talkgroup.id"
                      :talkgroup="repeater.ts2_dynamic_talkgroup" />
                    <span v-else class="text-sm text-muted-foreground">None</span>
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <!-- Last Heard -->
      <Card>
        <CardHeader>
          <CardTitle>Last Heard</CardTitle>
          <CardDescription>Recent calls through this repeater</CardDescription>
        </CardHeader>
        <CardContent>
          <DataTable
            :columns="columns"
            :data="lastheard"
            :loading="lastheardLoading"
            :loading-text="'Loading...'"
            :empty-text="'No calls found'"
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
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { DataTable } from '@/components/ui/data-table';
import User from '@/components/User.vue';
import Talkgroup from '@/components/Talkgroup.vue';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';

import API from '@/services/API';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

type TalkgroupRef = {
  id: number;
  name?: string;
};

type UserRef = {
  id: number;
  callsign: string;
};

type RepeaterData = {
  id: number;
  callsign: string;
  type: string;
  connected_time: string;
  last_ping_time: string;
  created_at: string;
  rx_frequency: number;
  tx_frequency: number;
  tx_power: number;
  color_code: number;
  latitude: number;
  longitude: number;
  height: number;
  location: string;
  city: string;
  state: string;
  country: string;
  description: string;
  url: string;
  software_id: string;
  package_id: string;
  slots: number;
  simplex_repeater: boolean;
  owner: UserRef;
  ts1_static_talkgroups: TalkgroupRef[];
  ts2_static_talkgroups: TalkgroupRef[];
  ts1_dynamic_talkgroup: TalkgroupRef;
  ts2_dynamic_talkgroup: TalkgroupRef;
};

type LastHeardRow = {
  id: number;
  active: boolean;
  start_time: string | Date;
  time_slot: boolean;
  user: UserRef;
  is_to_talkgroup: boolean;
  to_talkgroup: TalkgroupRef;
  is_to_repeater: boolean;
  to_repeater: { id: number; callsign: string };
  is_to_user: boolean;
  to_user: UserRef;
  duration: string | number;
  ber: string | number;
  loss: string | number;
  jitter: string | number;
  rssi: number;
};

export default {
  components: {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    Separator,
    Skeleton,
    DataTable,
    User,
    Talkgroup,
    RelativeTimestamp,
  },
  head: {
    title: 'Repeater Details',
  },
  created() {},
  mounted() {
    const id = this.$route.params.id;
    if (id) {
      this.repeaterID = Number(id);
      this.fetchRepeater();
      this.fetchLastheard();
      this.socket = ws.connect(getWebsocketURI() + '/calls', this.onWebsocketMessage);
      this.timeInterval = setInterval(() => { this.now = Date.now(); }, 30000);
    }
  },
  unmounted() {
    if (this.timeInterval !== null) {
      clearInterval(this.timeInterval);
    }
    if (this.socket) {
      this.socket.close();
    }
  },
  data: function() {
    return {
      repeaterID: 0,
      repeater: null as RepeaterData | null,
      loading: true,
      error: '',
      lastheard: [] as LastHeardRow[],
      lastheardLoading: false,
      totalRecords: 0,
      page: 1,
      rows: 30,
      socket: null as { close(): void } | null,
      now: Date.now(),
      timeInterval: null as ReturnType<typeof setInterval> | null,
    };
  },
  computed: {
    isOnline(): boolean {
      if (!this.repeater) return false;
      return this.hasTimestamp(this.repeater.last_ping_time);
    },
    hasCoordinates(): boolean {
      if (!this.repeater) return false;
      return this.repeater.latitude !== 0 || this.repeater.longitude !== 0;
    },
    columns() {
      void this.now;
      return [
        {
          accessorKey: 'time',
          header: 'Time',
          cell: ({ row }: { row: { original: LastHeardRow } }) => {
            const call = row.original;
            if (call.active) {
              return 'Active';
            }
            return h(RelativeTimestamp, { time: call.start_time, active: call.active });
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
            return h(User, { user: row.original.user });
          },
        },
        {
          accessorKey: 'destination',
          header: 'Destination',
          cell: ({ row }: { row: { original: LastHeardRow } }) => {
            const call = row.original;
            if (call.is_to_talkgroup) {
              return h(Talkgroup, { talkgroup: call.to_talkgroup });
            }
            if (call.is_to_repeater) {
              return `${call.to_repeater.callsign} | ${call.to_repeater.id}`;
            }
            if (call.is_to_user) {
              return h(User, { user: call.to_user });
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
    totalPages(): number {
      if (!this.totalRecords || this.totalRecords <= 0) {
        return 1;
      }
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
  },
  methods: {
    hasTimestamp(value: string | undefined): boolean {
      if (!value) return false;
      const d = new Date(value);
      return d.getFullYear() > 1;
    },
    formatFrequency(hz: number): string {
      if (!hz) return '-';
      const mhz = hz / 1000000;
      return `${mhz.toFixed(4)} MHz`;
    },
    fetchRepeater() {
      this.loading = true;
      API.get(`/repeaters/${this.repeaterID}`)
        .then((res) => {
          this.repeater = res.data;
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.error = 'Failed to load repeater details. You may not have permission to view this repeater.';
          this.loading = false;
        });
    },
    fetchLastheard(page = 1, limit = 30) {
      this.lastheardLoading = true;
      API.get(`/lastheard/repeater/${this.repeaterID}?page=${page}&limit=${limit}`)
        .then((res) => {
          this.totalRecords = res.data.total;
          this.lastheard = this.cleanData(res.data.calls);
          this.lastheardLoading = false;
        })
        .catch((err) => {
          console.error(err);
          this.lastheardLoading = false;
        });
    },
    cleanData(data: Record<string, unknown>[]) {
      if (!data) return [];
      const copyData = JSON.parse(JSON.stringify(data));
      for (let i = 0; i < copyData.length; i++) {
        copyData[i].start_time = new Date(copyData[i].start_time);
        if (typeof copyData[i].duration == 'number') {
          copyData[i].duration = (copyData[i].duration / 1000000000).toFixed(1);
        }
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
    handlePageIndexUpdate(pageIndex: number) {
      const nextPage = pageIndex + 1;
      if (nextPage === this.page || nextPage < 1) {
        return;
      }
      this.page = nextPage;
      this.fetchLastheard(this.page, this.rows);
    },
    handlePageSizeUpdate(pageSize: number) {
      if (pageSize === this.rows || pageSize <= 0) {
        return;
      }
      this.rows = pageSize;
      this.page = 1;
      this.fetchLastheard(this.page, this.rows);
    },
    onWebsocketMessage(event: MessageEvent) {
      const call = JSON.parse(event.data);
      // Only process calls relevant to this repeater
      if (call.repeater_id !== this.repeaterID &&
          !(call.is_to_repeater && call.to_repeater?.id === this.repeaterID)) {
        return;
      }
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
