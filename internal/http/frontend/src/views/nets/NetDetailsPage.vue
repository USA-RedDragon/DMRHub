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
                <dd class="detail-value"><User :user="net.started_by_user" /></dd>
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

      <!-- Check-ins -->
      <Card>
        <CardHeader>
          <CardTitle>Check-Ins</CardTitle>
          <CardDescription>Users who checked in during this net</CardDescription>
        </CardHeader>
        <CardContent>
          <div v-if="checkInsLoading" class="text-muted-foreground text-sm">Loading check-ins...</div>
          <div v-else-if="uniqueUsers.length === 0" class="text-muted-foreground text-sm">No check-ins yet</div>
          <div v-else class="flex flex-wrap gap-3">
            <User
              v-for="user in uniqueUsers"
              :key="user.id"
              :user="user"
            />
          </div>
        </CardContent>
      </Card>
    </template>
  </div>
</template>

<script lang="ts">
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
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
  type NetUser,
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
    ShadButton,
    RelativeTimestamp,
    User,
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
      socket: null as { close(): void } | null,
    };
  },
  computed: {
    canControl(): boolean {
      const userStore = useUserStore();
      return userStore.loggedIn;
    },
    uniqueUsers(): NetUser[] {
      const seen = new Set<number>();
      const users: NetUser[] = [];
      for (const ci of this.checkIns) {
        if (!seen.has(ci.user.id)) {
          seen.add(ci.user.id);
          users.push(ci.user);
        }
      }
      return users;
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
    fetchCheckIns() {
      this.checkInsLoading = true;
      getNetCheckIns(this.netID, { limit: 10000 })
        .then((res) => {
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

    onWebsocketMessage(event: MessageEvent) {
      const data = JSON.parse(event.data);

      // Check-in event (from net:checkins:{id} topic)
      if (data.call_id && data.user) {
        const newCheckIn: NetCheckIn = {
          call_id: data.call_id,
          user: data.user,
          repeater_id: data.repeater_id ?? 0,
          start_time: data.start_time,
        };
        // Avoid duplicate call entries.
        const exists = this.checkIns.some((ci) => ci.call_id === newCheckIn.call_id);
        if (!exists) {
          this.checkIns = [newCheckIn, ...this.checkIns];
        }
        // Update the check-in count on the net (unique users).
        if (this.net) {
          this.net.check_in_count = this.uniqueUsers.length;
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
