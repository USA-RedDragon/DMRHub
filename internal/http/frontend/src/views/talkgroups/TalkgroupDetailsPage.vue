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
          <div class="grid gap-4 md:grid-cols-2">
            <Skeleton v-for="n in 4" :key="n" class="h-16" />
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

    <!-- Talkgroup details -->
    <template v-else-if="talkgroup">
      <!-- Header card -->
      <Card>
        <CardHeader>
          <div class="flex items-center justify-between">
            <div>
              <CardTitle>TG {{ talkgroup.id }} — {{ talkgroup.name }}</CardTitle>
              <CardDescription v-if="talkgroup.description">{{ talkgroup.description }}</CardDescription>
            </div>
          </div>
        </CardHeader>
      </Card>

      <!-- Info grid -->
      <div class="grid gap-6 md:grid-cols-2">
        <!-- Talkgroup Information -->
        <Card>
          <CardHeader>
            <CardTitle>Talkgroup Information</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div class="detail-row">
                <dt class="detail-label">Channel</dt>
                <dd class="detail-value">{{ talkgroup.id }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Name</dt>
                <dd class="detail-value">{{ talkgroup.name }}</dd>
              </div>
              <div v-if="talkgroup.description" class="detail-row">
                <dt class="detail-label">Description</dt>
                <dd class="detail-value">{{ talkgroup.description }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Created</dt>
                <dd class="detail-value">
                  <RelativeTimestamp :time="talkgroup.created_at" />
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <!-- Admins & NCOs -->
        <Card>
          <CardHeader>
            <CardTitle>Management</CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-4">
              <div>
                <h4 class="text-sm font-semibold mb-2">Admins</h4>
                <div v-if="talkgroup.admins && talkgroup.admins.length > 0" class="flex flex-wrap gap-2">
                  <User v-for="admin in talkgroup.admins" :key="admin.id" :user="admin" />
                </div>
                <p v-else class="text-sm text-muted-foreground">None</p>
              </div>
              <Separator />
              <div>
                <h4 class="text-sm font-semibold mb-2">Net Control Operators</h4>
                <div v-if="talkgroup.ncos && talkgroup.ncos.length > 0" class="flex flex-wrap gap-2">
                  <User v-for="nco in talkgroup.ncos" :key="nco.id" :user="nco" />
                </div>
                <p v-else class="text-sm text-muted-foreground">None</p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <!-- Net Control -->
      <Card>
        <CardHeader>
          <div class="flex items-center justify-between">
            <CardTitle>Net Control</CardTitle>
            <div class="flex gap-2">
              <RouterLink v-if="!activeNet" :to="`/nets/scheduled/new?talkgroup_id=${talkgroupID}`">
                <Button variant="outline" size="sm">Schedule Net</Button>
              </RouterLink>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <!-- Active Net -->
          <div v-if="activeNet" class="space-y-3">
            <div class="flex items-center gap-2">
              <span class="inline-block h-2 w-2 rounded-full bg-green-500 animate-pulse" />
              <span class="text-sm font-semibold text-green-600">Active Net</span>
            </div>
            <p v-if="activeNet.description" class="text-sm text-muted-foreground">{{ activeNet.description }}</p>
            <div class="flex items-center gap-4 text-sm">
              <span>Started by {{ activeNet.started_by_user.callsign }}</span>
              <span>{{ activeNet.check_in_count + recentCheckIns.length }} check-ins</span>
            </div>

            <!-- Recent check-ins panel -->
            <div v-if="recentCheckIns.length > 0" class="mt-2 rounded-md border p-3 space-y-1">
              <h4 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1">Recent Check-Ins</h4>
              <div
                v-for="ci in recentCheckIns.slice(0, 5)"
                :key="ci.call_id"
                class="flex items-center justify-between text-sm"
              >
                <User :user="ci.user" />
                <span class="text-xs text-muted-foreground">
                  {{ formatDuration(ci.duration) }}
                </span>
              </div>
              <div v-if="recentCheckIns.length > 5" class="text-xs text-muted-foreground pt-1">
                and {{ recentCheckIns.length - 5 }} more...
              </div>
            </div>

            <div class="flex gap-2">
              <RouterLink :to="`/nets/${activeNet.id}`">
                <Button variant="outline" size="sm">View Check-Ins</Button>
              </RouterLink>
              <Button variant="destructive" size="sm" @click="handleStopNet">Stop Net</Button>
            </div>
          </div>

          <!-- No active net -->
          <div v-else class="space-y-3">
            <p class="text-sm text-muted-foreground">No active net on this talkgroup.</p>
            <div class="flex gap-2">
              <Button variant="outline" size="sm" @click="showStartNet = !showStartNet">
                {{ showStartNet ? 'Cancel' : 'Start Net' }}
              </Button>
            </div>
            <!-- Inline start form -->
            <div v-if="showStartNet" class="space-y-2 pt-2">
              <ShadInput
                type="text"
                v-model="netDescription"
                placeholder="Net description (optional)"
              />
              <ShadInput
                type="number"
                v-model="netDuration"
                placeholder="Duration in minutes (optional)"
                min="1"
                max="1440"
              />
              <Button size="sm" @click="handleStartNet">Start</Button>
            </div>
          </div>

          <!-- Scheduled Nets -->
          <div v-if="scheduledNets.length > 0" class="mt-4 pt-4 border-t">
            <h4 class="text-sm font-semibold mb-2">Scheduled Nets</h4>
            <ul class="space-y-1">
              <li v-for="sn in scheduledNets" :key="sn.id" class="text-sm flex items-center justify-between">
                <span>
                  <strong>{{ sn.name }}</strong> —
                  {{ dayNames[sn.day_of_week] }} {{ sn.time_of_day }} UTC
                  <span v-if="sn.duration_minutes">({{ sn.duration_minutes }}m)</span>
                  <span v-if="!sn.enabled" class="text-muted-foreground"> (disabled)</span>
                </span>
                <RouterLink :to="`/nets/scheduled/${sn.id}/edit`">
                  <ShadButton variant="ghost" size="sm">Edit</ShadButton>
                </RouterLink>
              </li>
            </ul>
          </div>
        </CardContent>
      </Card>

      <!-- Last Heard -->
      <Card>
        <CardHeader>
          <CardTitle>Last Heard</CardTitle>
          <CardDescription>Recent calls on this talkgroup</CardDescription>
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
import { Button as ShadButton } from '@/components/ui/button';
import { Input as ShadInput } from '@/components/ui/input';
import User from '@/components/User.vue';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';

import API from '@/services/API';
import {
  getNets,
  startNet,
  stopNet as stopNetAPI,
  getScheduledNets,
  type Net,
  type ScheduledNet,
} from '@/services/net';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

type UserRef = {
  id: number;
  callsign: string;
};

type TalkgroupRef = {
  id: number;
  name?: string;
};

type RepeaterRef = {
  id: number;
  callsign: string;
};

type TalkgroupData = {
  id: number;
  name: string;
  description: string;
  admins: UserRef[];
  ncos: UserRef[];
  created_at: string;
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
  to_repeater: RepeaterRef;
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
    ShadButton,
    ShadInput,
    User,
    RelativeTimestamp,
  },
  head: {
    title: 'Talkgroup Details',
  },
  created() {},
  mounted() {
    const id = this.$route.params.id;
    if (id) {
      this.talkgroupID = Number(id);
      this.fetchTalkgroup();
      this.fetchLastheard();
      this.fetchActiveNet();
      this.fetchScheduledNets();
      this.socket = ws.connect(getWebsocketURI() + '/calls', this.onWebsocketMessage);
      this.netSocket = ws.connect(getWebsocketURI() + '/nets', this.onNetWebsocketMessage);
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
    if (this.netSocket) {
      this.netSocket.close();
    }
  },
  data: function() {
    return {
      talkgroupID: 0,
      talkgroup: null as TalkgroupData | null,
      loading: true,
      error: '',
      lastheard: [] as LastHeardRow[],
      lastheardLoading: false,
      totalRecords: 0,
      page: 1,
      rows: 30,
      socket: null as { close(): void } | null,
      netSocket: null as { close(): void } | null,
      now: Date.now(),
      timeInterval: null as ReturnType<typeof setInterval> | null,
      activeNet: null as Net | null,
      recentCheckIns: [] as { call_id: number; user: { id: number; callsign: string }; start_time: string; duration: number }[],
      scheduledNets: [] as ScheduledNet[],
      showStartNet: false,
      netDescription: '',
      netDuration: '' as string | number,
      dayNames: ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'],
    };
  },
  computed: {
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
    fetchTalkgroup() {
      this.loading = true;
      API.get(`/talkgroups/${this.talkgroupID}`)
        .then((res) => {
          this.talkgroup = res.data;
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.error = 'Failed to load talkgroup details.';
          this.loading = false;
        });
    },
    fetchLastheard(page = 1, limit = 30) {
      this.lastheardLoading = true;
      API.get(`/lastheard/talkgroup/${this.talkgroupID}?page=${page}&limit=${limit}`)
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
      // Only process calls to this talkgroup
      if (!call.is_to_talkgroup || call.to_talkgroup?.id !== this.talkgroupID) {
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
    onNetWebsocketMessage(event: MessageEvent) {
      const data = JSON.parse(event.data);

      // Check-in event (from net:checkins:{id} topic)
      if (data.call_id && data.user) {
        const exists = this.recentCheckIns.some((ci: { call_id: number }) => ci.call_id === data.call_id);
        if (!exists) {
          this.recentCheckIns = [data, ...this.recentCheckIns];
        }
        return;
      }

      // Net lifecycle event
      if (data.talkgroup_id === this.talkgroupID) {
        this.fetchActiveNet();
        if (data.event === 'stopped') {
          this.recentCheckIns = [];
        }
      }
    },
    fetchActiveNet() {
      getNets({ talkgroup_id: this.talkgroupID, active: true, limit: 1 })
        .then((res) => {
          const nets = res.data.nets || [];
          const previousNetID = this.activeNet?.id;
          this.activeNet = (nets.length > 0 ? nets[0] : null) ?? null;

          // If we have an active net and its ID changed (or first load),
          // reconnect the net websocket with the net_id for live check-ins.
          if (this.activeNet && this.activeNet.id !== previousNetID) {
            this.recentCheckIns = [];
            if (this.netSocket) {
              this.netSocket.close();
            }
            this.netSocket = ws.connect(
              getWebsocketURI() + '/nets?net_id=' + this.activeNet.id,
              this.onNetWebsocketMessage,
            );
          } else if (!this.activeNet && previousNetID) {
            // Net ended — reconnect without net_id.
            this.recentCheckIns = [];
            if (this.netSocket) {
              this.netSocket.close();
            }
            this.netSocket = ws.connect(getWebsocketURI() + '/nets', this.onNetWebsocketMessage);
          }
        })
        .catch((err) => console.error(err));
    },
    fetchScheduledNets() {
      getScheduledNets({ talkgroup_id: this.talkgroupID })
        .then((res) => {
          this.scheduledNets = res.data.scheduled_nets || [];
        })
        .catch((err) => console.error(err));
    },
    handleStartNet() {
      const dur = this.netDuration !== '' ? Number(this.netDuration) : undefined;
      startNet({
        talkgroup_id: this.talkgroupID,
        description: this.netDescription.trim() || undefined,
        duration_minutes: dur,
      })
        .then((res) => {
          this.activeNet = res.data;
          this.showStartNet = false;
          this.netDescription = '';
          this.netDuration = '';
          this.recentCheckIns = [];
          // Reconnect WS with the new net_id.
          if (this.netSocket) {
            this.netSocket.close();
          }
          this.netSocket = ws.connect(
            getWebsocketURI() + '/nets?net_id=' + res.data.id,
            this.onNetWebsocketMessage,
          );
        })
        .catch((err) => console.error(err));
    },
    handleStopNet() {
      if (!this.activeNet) return;
      stopNetAPI(this.activeNet.id)
        .then(() => {
          this.activeNet = null;
          this.recentCheckIns = [];
        })
        .catch((err) => console.error(err));
    },
    formatDuration(ns: number): string {
      return (ns / 1000000000).toFixed(1) + 's';
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
