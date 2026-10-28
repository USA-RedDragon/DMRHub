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
                {{ protocol || (repeater.type || 'mmdvm').toUpperCase() }}
              </span>
              <span v-if="repeater.simplex_repeater"
                class="inline-flex items-center rounded-md bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
                Simplex
              </span>
              <ShadButton v-if="canConnect" as-child variant="outline" size="sm" data-test="connect">
                <RouterLink :to="`/repeaters/${repeater.id}/connect`" class="no-underline">Connect</RouterLink>
              </ShadButton>
            </div>
          </div>
        </CardHeader>
      </Card>

      <Card v-if="repeater.type !== 'ipsc'" data-test="voice-codec">
        <CardHeader>
          <CardTitle>Voice codec</CardTitle>
          <CardDescription>
            The codec this repeater's radios use on each timeslot. DMRHub sends AMBE calls to a Codec2 timeslot as
            Codec2, and Codec2 calls to AMBE targets as AMBE.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="grid gap-4 md:grid-cols-2">
            <div v-for="slot in codecSlots" :key="slot.key" class="flex flex-col gap-2 rounded-lg border p-4">
              <h4 class="text-sm font-semibold">{{ slot.title }}</h4>
              <div class="flex flex-col gap-1">
                <label class="text-sm" :for="`${slot.key}-codec`">Codec</label>
                <ShadSelect
                  :model-value="codecs[slot.key]"
                  :disabled="!canEditCodec"
                  @update:model-value="(v) => setCodec(slot.key, v)"
                >
                  <SelectTrigger :id="`${slot.key}-codec`" class="w-full" :data-test="`${slot.key}-codec`">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="opt in codecOptions" :key="opt.value" :value="opt.value">
                      {{ opt.label }}
                    </SelectItem>
                  </SelectContent>
                </ShadSelect>
              </div>
              <p
                v-if="codecs[slot.key] === 'auto' && slot.heard"
                class="text-sm text-muted-foreground"
                :data-test="`${slot.key}-heard`"
              >{{ slot.heard }}</p>
            </div>
          </div>
          <p class="text-sm text-muted-foreground">
            Automatic follows the last call heard on the timeslot. A timeslot counts as Codec2 only when a call starts
            with a Codec2 voice header; until then it is AMBE.
          </p>
          <div v-if="canEditCodec" class="flex items-center gap-3">
            <ShadButton type="button" :disabled="codecSaving" data-test="codec-save" @click="saveCodecs">Save</ShadButton>
            <span
              v-if="codecMessage"
              class="text-sm"
              :class="codecFailed ? 'text-destructive' : 'text-muted-foreground'"
              data-test="codec-message"
            >{{ codecMessage }}</span>
          </div>
        </CardContent>
      </Card>

      <RepeaterRulesCard v-if="canEditCodec" :repeater-id="repeater.id" />

      <!-- Info grid -->
      <div class="grid gap-6 md:grid-cols-2">
        <!-- Radio Configuration -->
        <Card>
          <CardHeader>
            <CardTitle>Radio Configuration</CardTitle>
          </CardHeader>
          <CardContent>
            <dl class="detail-list">
              <div v-if="protocol" class="detail-row">
                <dt class="detail-label">Protocol</dt>
                <dd class="detail-value">{{ protocol }}</dd>
              </div>
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
              <div v-if="repeater.slots" class="detail-row">
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
        <CardContent class="flex flex-col gap-4">
          <TypeFilter v-model="filter" />
          <DataTable
            :columns="columns"
            :data="lastheard"
            :loading="lastheardLoading"
            :loading-text="'Loading...'"
            :empty-text="'No transmissions match these types'"
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
import type { AcceptableValue } from 'reka-ui';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select as ShadSelect,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { DataTable } from '@/components/ui/data-table';
import TypeFilter from '@/components/TypeFilter.vue';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import RepeaterRulesCard from '@/components/RepeaterRulesCard.vue';
import Talkgroup from '@/components/Talkgroup.vue';
import User from '@/components/User.vue';
import { lastHeardColumns } from '@/lib/lastHeardColumns';
import { connectKind } from '@/lib/enroll';
import { heardLine, type Codec, type Codec2Mode, type SlotCodec } from '@/lib/codec';
import {
  cleanCalls,
  defaultFilter,
  filterQuery,
  matchesFilter,
  type CallRow,
  type TypeFilterValue,
} from '@/lib/transmissions';

import API from '@/services/API';
import { errorText } from '@/services/settings';
import { getWebsocketURI } from '@/services/util';
import { useUserStore } from '@/store';
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
  dialect: string;
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
  ts1_codec: SlotCodec;
  ts2_codec: SlotCodec;
  ts1_heard_codec: Codec;
  ts2_heard_codec: Codec;
  ts1_heard_codec2_mode: Codec2Mode;
  ts2_heard_codec2_mode: Codec2Mode;
  ts1_heard_at: string | null;
  ts2_heard_at: string | null;
};

type SlotKey = 'ts1' | 'ts2';

/** SESSION_TIMEOUT_MS is how long after its last ping the hub keeps a repeater logged in. */
const SESSION_TIMEOUT_MS = 60_000;

const CODEC_OPTIONS: { value: SlotCodec; label: string }[] = [
  { value: 'auto', label: 'Automatic' },
  { value: 'codec2', label: 'Codec2' },
  { value: 'ambe', label: 'AMBE' },
];

const slotCodec = (v: unknown): SlotCodec => (v === 'codec2' || v === 'ambe' ? v : 'auto');

export default {
  components: {
    ShadButton,
    ShadSelect,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    Separator,
    Skeleton,
    DataTable,
    TypeFilter,
    RelativeTimestamp,
    RepeaterRulesCard,
    Talkgroup,
    User,
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
      lastheard: [] as CallRow[],
      lastheardLoading: false,
      filter: defaultFilter() as TypeFilterValue,
      totalRecords: 0,
      page: 1,
      rows: 30,
      socket: null as { close(): void } | null,
      now: Date.now(),
      timeInterval: null as ReturnType<typeof setInterval> | null,
      codecOptions: CODEC_OPTIONS,
      codecs: { ts1: 'auto', ts2: 'auto' } as Record<SlotKey, SlotCodec>,
      codecSaving: false,
      codecMessage: '',
      codecFailed: false,
    };
  },
  watch: {
    filter: {
      deep: true,
      handler() {
        this.page = 1;
        this.fetchLastheard(this.page, this.rows);
      },
    },
  },
  computed: {
    isOnline(): boolean {
      if (!this.repeater || !this.hasTimestamp(this.repeater.last_ping_time)) return false;
      return this.now - new Date(this.repeater.last_ping_time).getTime() < SESSION_TIMEOUT_MS;
    },
    protocol(): string {
      if (!this.repeater || (this.repeater.type || 'mmdvm') !== 'mmdvm') return '';
      return this.repeater.dialect === 'HBRP' ? 'HBRP' : 'MMDVM';
    },
    canConnect(): boolean {
      return this.canEditCodec && !!this.repeater && connectKind(this.repeater.type || 'mmdvm', this.repeater.id) !== null;
    },
    canEditCodec(): boolean {
      const userStore = useUserStore();
      if (!this.repeater || !userStore.loggedIn) return false;
      return userStore.admin || (!!this.repeater.owner && this.repeater.owner.id === userStore.id);
    },
    codecSlots(): { key: SlotKey; title: string; heard: string }[] {
      const r = this.repeater;
      return [
        {
          key: 'ts1',
          title: 'Timeslot 1',
          heard: r ? heardLine(r.ts1_heard_codec, r.ts1_heard_codec2_mode, r.ts1_heard_at) : '',
        },
        {
          key: 'ts2',
          title: 'Timeslot 2',
          heard: r ? heardLine(r.ts2_heard_codec, r.ts2_heard_codec2_mode, r.ts2_heard_at) : '',
        },
      ];
    },
    hasCoordinates(): boolean {
      if (!this.repeater) return false;
      return this.repeater.latitude !== 0 || this.repeater.longitude !== 0;
    },
    columns() {
      void this.now;
      return lastHeardColumns({});
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
          this.codecs = { ts1: slotCodec(res.data?.ts1_codec), ts2: slotCodec(res.data?.ts2_codec) };
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.error = 'Failed to load repeater details. You may not have permission to view this repeater.';
          this.loading = false;
        });
    },
    setCodec(slot: SlotKey, value: AcceptableValue) {
      this.codecs = { ...this.codecs, [slot]: slotCodec(value) };
      this.codecMessage = '';
      this.codecFailed = false;
    },
    saveCodecs() {
      const body = { ts1_codec: this.codecs.ts1, ts2_codec: this.codecs.ts2 };
      this.codecSaving = true;
      this.codecMessage = '';
      this.codecFailed = false;
      API.patch(`/repeaters/${this.repeaterID}`, body)
        .then(() => {
          if (this.repeater) {
            this.repeater = { ...this.repeater, ...body };
          }
          this.codecMessage = 'Saved.';
        })
        .catch((err) => {
          console.error(err);
          this.codecFailed = true;
          this.codecMessage = errorText(err, 'Could not save. Try again later.');
        })
        .finally(() => {
          this.codecSaving = false;
        });
    },
    fetchLastheard(page = 1, limit = 30) {
      if (this.filter.types.length === 0) {
        this.lastheard = [];
        this.totalRecords = 0;
        return;
      }
      this.lastheardLoading = true;
      API.get(`/lastheard/repeater/${this.repeaterID}?page=${page}&limit=${limit}&${filterQuery(this.filter)}`)
        .then((res) => {
          this.totalRecords = res.data.total;
          this.lastheard = cleanCalls(res.data.calls);
          this.lastheardLoading = false;
        })
        .catch((err) => {
          console.error(err);
          this.lastheardLoading = false;
        });
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
      if (!matchesFilter(call, this.filter) && !this.lastheard.some((c) => c.id === call.id)) {
        return;
      }
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
      this.lastheard = cleanCalls(copyLastheard);
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
