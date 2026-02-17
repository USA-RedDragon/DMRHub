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

    <!-- User details -->
    <template v-else-if="user">
      <!-- Header card -->
      <Card>
        <CardHeader>
          <div class="flex items-center justify-between">
            <div>
              <CardTitle>
                <span v-if="radioIdData && radioIdData.flag" class="cursor-default text-lg leading-none">{{ radioIdData.flag }}&nbsp;</span>
                {{ user.callsign }} — {{ user.id }}
              </CardTitle>
              <CardDescription v-if="radioIdData">
                {{ radioIdData.name }} {{ radioIdData.surname }}
              </CardDescription>
            </div>
            <div class="flex items-center gap-2">
              <span v-if="user.admin"
                class="inline-flex items-center rounded-md bg-blue-500/10 px-2.5 py-0.5 text-xs font-medium text-blue-500">
                Admin
              </span>
              <span class="inline-flex items-center rounded-md px-2.5 py-0.5 text-xs font-medium"
                :class="user.approved ? 'bg-green-500/10 text-green-500' : 'bg-yellow-500/10 text-yellow-500'">
                {{ user.approved ? 'Approved' : 'Pending Approval' }}
              </span>
            </div>
          </div>
        </CardHeader>
      </Card>

      <!-- Info grid -->
      <div class="grid gap-6 md:grid-cols-2">
        <!-- User Information -->
        <Card>
          <CardHeader>
            <CardTitle>User Information</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div class="detail-row">
                <dt class="detail-label">DMR ID</dt>
                <dd class="detail-value">{{ user.id }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Callsign</dt>
                <dd class="detail-value">{{ user.callsign }}</dd>
              </div>
              <div v-if="radioIdData && radioIdData.name" class="detail-row">
                <dt class="detail-label">Name</dt>
                <dd class="detail-value">{{ radioIdData.name }} {{ radioIdData.surname }}</dd>
              </div>
              <div class="detail-row">
                <dt class="detail-label">Registered</dt>
                <dd class="detail-value">
                  <RelativeTimestamp :time="user.created_at" />
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <!-- Location (from RadioID) -->
        <Card v-if="radioIdData && (radioIdData.city || radioIdData.state || radioIdData.country)">
          <CardHeader>
            <CardTitle>Location</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div v-if="radioIdData.city" class="detail-row">
                <dt class="detail-label">City</dt>
                <dd class="detail-value">{{ radioIdData.city }}</dd>
              </div>
              <div v-if="radioIdData.state" class="detail-row">
                <dt class="detail-label">State</dt>
                <dd class="detail-value">{{ radioIdData.state }}</dd>
              </div>
              <div v-if="radioIdData.country" class="detail-row">
                <dt class="detail-label">Country</dt>
                <dd class="detail-value">
                  <span v-if="radioIdData.flag">{{ radioIdData.flag }}&nbsp;</span>{{ radioIdData.country }}
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        <!-- Repeaters -->
        <Card>
          <CardHeader>
            <CardTitle>Repeaters</CardTitle>
            <CardDescription>Repeaters owned by this user</CardDescription>
          </CardHeader>
          <CardContent>
            <div v-if="user.repeaters && user.repeaters.length > 0" class="space-y-2">
              <RouterLink
                v-for="repeater in user.repeaters"
                :key="repeater.id"
                :to="`/repeaters/${repeater.id}`"
                class="flex items-center justify-between rounded-md border px-4 py-2 text-sm hover:bg-muted transition-colors"
              >
                <span class="font-medium">{{ repeater.callsign || repeater.id }}</span>
                <span class="text-muted-foreground">{{ repeater.id }}</span>
              </RouterLink>
            </div>
            <p v-else class="text-sm text-muted-foreground">No repeaters registered</p>
          </CardContent>
        </Card>
      </div>

      <!-- Last Heard -->
      <Card>
        <CardHeader>
          <CardTitle>Last Heard</CardTitle>
          <CardDescription>Recent calls by this user</CardDescription>
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
import { RouterLink } from 'vue-router';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { DataTable } from '@/components/ui/data-table';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import Talkgroup from '@/components/Talkgroup.vue';
import User from '@/components/User.vue';

import API from '@/services/API';
import { getUserDB, type RadioIdData } from '@/services/userdb';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';
import { useUserStore } from '@/store';

type TalkgroupRef = {
  id: number;
  name?: string;
};

type UserRef = {
  id: number;
  callsign: string;
};

type RepeaterRef = {
  id: number;
  callsign: string;
};

type UserProfile = {
  id: number;
  callsign: string;
  admin: boolean;
  approved: boolean;
  repeaters: RepeaterRef[];
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
    Skeleton,
    DataTable,
    RelativeTimestamp,
    RouterLink,
  },
  head: {
    title: 'User Details',
  },
  created() {},
  mounted() {
    const userStore = useUserStore();
    if (!userStore.loggedIn) {
      this.$router.replace('/');
      return;
    }
    const id = this.$route.params.id;
    if (id) {
      this.userID = Number(id);
      this.fetchUser();
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
      userID: 0,
      user: null as UserProfile | null,
      radioIdData: null as RadioIdData | null,
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
    fetchUser() {
      this.loading = true;
      API.get(`/users/${this.userID}/profile`)
        .then((res) => {
          this.user = res.data;
          this.loading = false;
          getUserDB(this.userID).then((data) => {
            this.radioIdData = data;
          });
        })
        .catch((err) => {
          console.error(err);
          this.error = 'Failed to load user details. You may not have permission to view this user.';
          this.loading = false;
        });
    },
    fetchLastheard(page = 1, limit = 30) {
      this.lastheardLoading = true;
      API.get(`/lastheard/user/${this.userID}?page=${page}&limit=${limit}`)
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
      // Only process calls from this user
      if (call.user?.id !== this.userID) {
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
