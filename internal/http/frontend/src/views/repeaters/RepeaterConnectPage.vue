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
  <div class="mx-auto flex max-w-[980px] flex-col gap-5">
    <Card v-if="error">
      <CardContent>
        <p class="text-destructive">{{ error }}</p>
      </CardContent>
    </Card>

    <template v-else-if="repeater && kind">
      <div>
        <div class="mb-1.5 text-[13px] text-muted-foreground">
          <RouterLink to="/repeaters">Repeaters</RouterLink> / <span>{{ repeater.id }}</span>
        </div>
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="min-w-0 flex-[1_1_20rem]">
            <h1 class="mb-1.5 text-[26px] leading-tight font-bold" data-test="title">{{ title }}</h1>
            <p class="text-muted-foreground" data-test="lead">{{ lead }}</p>
          </div>
          <span
            class="mt-1.5 inline-flex items-center gap-1.5 rounded-full border px-2.5 text-xs leading-[22px] font-medium whitespace-nowrap"
            :class="connected ? 'border-success/55' : ''"
            data-test="live"
          >
            <span class="size-[7px] shrink-0 rounded-full" :class="connected ? 'bg-success' : 'bg-muted-foreground'" />
            <span>{{ liveText }}</span>
          </span>
        </div>
      </div>

      <Card class="gap-5">
        <CardHeader><CardTitle>Connection details</CardTitle></CardHeader>
        <CardContent>
          <div class="details overflow-hidden rounded-[10px] border" data-test="details">
            <template v-for="row in details" :key="row.label">
              <div class="details-k">{{ row.label }}</div>
              <div class="details-v justify-between">
                <span class="font-mono text-base break-all">{{ row.value }}</span>
                <CopyButton :text="row.value" />
              </div>
            </template>
            <div class="details-k">{{ secretLabel }}</div>
            <div v-if="secret" class="details-v justify-between">
              <span
                class="rounded-md border bg-muted px-2.5 py-1 font-mono text-xl font-semibold tracking-[0.04em] break-all"
                data-test="secret"
              >{{ secret }}</span>
              <CopyButton :text="secret" :label="`Copy ${secretWord}`" />
            </div>
            <div v-else class="details-v justify-between">
              <span class="text-muted-foreground" data-test="secret-hidden">Hidden</span>
              <ShadButton
                v-if="canRotate"
                type="button"
                variant="outline"
                size="sm"
                class="h-7 border-destructive-foreground/50 px-2.5 text-[13px] text-destructive-foreground"
                data-test="rotate"
                @click="confirming = true"
              >Rotate {{ secretWord }}</ShadButton>
            </div>
          </div>
          <template v-if="secret">
            <div class="mt-3.5 rounded-[10px] border border-warn-border bg-warn-bg px-3.5 py-3 text-sm text-warn-fg" data-test="copy-now">
              <b>Copy the {{ secretWord }} now. This page shows it only once.</b> When you leave this page, DMRHub does
              not show it again. If you lose it, open this {{ noun }} on the Repeaters page and click
              <b>Rotate {{ secretWord }}</b>. DMRHub then makes a new one and shows it once.
            </div>
            <div v-if="rotatedAt" class="mt-3.5 rounded-[10px] border bg-muted px-3.5 py-3 text-sm" data-test="rotated">
              The old {{ secretWord }} stopped working. DMRHub dropped the connection of the {{ noun }}. Put the new
              {{ secretWord }} in your software and it logs in again.
            </div>
          </template>
        </CardContent>
      </Card>

      <Card class="gap-5 pb-5">
        <CardHeader>
          <CardTitle>Set up your software</CardTitle>
          <CardDescription>Pick the program that runs your {{ noun }}.</CardDescription>
        </CardHeader>
        <Tabs v-model="tab" class="gap-0">
          <TabsList class="h-auto w-full justify-start gap-0.5 overflow-x-auto rounded-none border-b bg-transparent p-0 px-6">
            <TabsTrigger
              v-for="t in tabs"
              :key="t"
              :value="t"
              class="-mb-px h-auto flex-none rounded-none border-0 border-b-2 border-transparent px-3 py-2 font-semibold text-muted-foreground shadow-none data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:text-foreground data-[state=active]:shadow-none dark:text-muted-foreground dark:data-[state=active]:border-primary dark:data-[state=active]:bg-transparent"
              :data-test="`tab-${t}`"
            >
              {{ tabInfo[t].name }}
              <span v-if="tabInfo[t].tested === true" class="size-1.5 rounded-full bg-success" title="Tested" />
              <span v-else-if="tabInfo[t].tested === false" class="size-1.5 rounded-full bg-warn-border" title="Not tested" />
            </TabsTrigger>
          </TabsList>
          <TabsContent v-for="t in tabs" :key="t" :value="t" class="px-6 pt-5" :data-test="`panel-${t}`">
            <ClientSteps :tab="t" :values="values" @goto="tab = $event" />
          </TabsContent>
        </Tabs>
      </Card>

      <Card class="gap-5">
        <CardHeader><CardTitle>Next</CardTitle></CardHeader>
        <CardContent class="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3">
          <RouterLink
            v-for="n in next"
            :key="n.label"
            :to="n.to"
            class="no-underline block rounded-[10px] border px-3.5 py-3 font-bold text-inherit hover:bg-accent"
          >{{ n.label }}</RouterLink>
        </CardContent>
      </Card>

      <ShadDialog :open="confirming" @update:open="confirming = $event">
        <DialogContent data-test="rotate-dialog">
          <DialogTitle>Rotate the {{ secretWord }} of {{ repeater.id }}?</DialogTitle>
          <DialogDescription class="text-foreground">
            <b>The {{ noun }} disconnects at once.</b> It stays disconnected until you put the new {{ secretWord }} in
            its software and it logs in with it.
          </DialogDescription>
          <div class="flex flex-wrap justify-end gap-2">
            <ShadButton type="button" variant="outline" size="sm" data-test="rotate-cancel" @click="confirming = false">
              Cancel
            </ShadButton>
            <ShadButton
              type="button"
              variant="outline"
              size="sm"
              class="border-destructive-foreground/50 text-destructive-foreground"
              :disabled="rotating"
              data-test="rotate-confirm"
              @click="rotate"
            >Rotate {{ secretWord }}</ShadButton>
          </div>
        </DialogContent>
      </ShadDialog>
    </template>
  </div>
</template>

<script lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog as ShadDialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast';
import ClientSteps from '@/components/enroll/ClientSteps.vue';
import CopyButton from '@/components/enroll/CopyButton.vue';
import {
  CLIENT_TABS,
  clientTabs,
  connectKind,
  deviceNoun,
  essid,
  secretName,
  secretPlaceholder,
  takeSecret,
  type ClientTab,
  type ConnectKind,
  type ConnectValues,
} from '@/lib/enroll';
import API from '@/services/API';
import { errorText } from '@/services/settings';
import { useSettingsStore, useUserStore } from '@/store';
import { formatDistanceStrict } from 'date-fns';
import { RouterLink } from 'vue-router';

type RepeaterData = {
  id: number;
  type: string;
  connected_time: string;
  last_ping_time: string;
  owner?: { id: number; callsign: string };
};

/** SESSION_TIMEOUT_MS is how long after its last ping the hub keeps a repeater logged in. */
const SESSION_TIMEOUT_MS = 60_000;

/** POLL_MS is how often the page reads the repeater again for its live status. */
const POLL_MS = 5_000;

const timeOf = (s: string | undefined): number => {
  if (!s) return 0;
  const t = new Date(s).getTime();
  return Number.isNaN(t) || new Date(s).getFullYear() <= 1 ? 0 : t;
};

export default {
  components: {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    ClientSteps,
    CopyButton,
    ShadDialog,
    DialogContent,
    DialogDescription,
    DialogTitle,
    RouterLink,
    ShadButton,
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
  },
  head: {
    title: 'Connect',
  },
  data() {
    return {
      repeaterID: 0,
      repeater: null as RepeaterData | null,
      error: '',
      secret: '',
      firstVisit: false,
      rotatedAt: null as Date | null,
      confirming: false,
      rotating: false,
      tab: '' as ClientTab | '',
      now: Date.now(),
      poll: null as ReturnType<typeof setInterval> | null,
      clock: null as ReturnType<typeof setInterval> | null,
      tabInfo: CLIENT_TABS,
    };
  },
  mounted() {
    this.repeaterID = Number(this.$route.params.id);
    const kept = takeSecret(this.repeaterID);
    if (kept) {
      this.secret = kept;
      this.firstVisit = true;
    }
    useSettingsStore().fetchConfig();
    this.fetchRepeater();
    this.poll = setInterval(() => this.fetchRepeater(), POLL_MS);
    this.clock = setInterval(() => {
      this.now = Date.now();
    }, 1000);
  },
  unmounted() {
    if (this.poll !== null) clearInterval(this.poll);
    if (this.clock !== null) clearInterval(this.clock);
  },
  computed: {
    kind(): ConnectKind | null {
      return this.repeater ? connectKind(this.repeater.type || 'mmdvm', this.repeater.id) : null;
    },
    noun(): string {
      return this.kind ? deviceNoun(this.kind) : 'repeater';
    },
    secretWord(): string {
      return this.kind ? secretName(this.kind) : 'password';
    },
    secretLabel(): string {
      return this.kind === 'ipsc' ? 'Auth key' : 'Password';
    },
    tabs(): ClientTab[] {
      return this.kind ? clientTabs(this.kind) : [];
    },
    port(): number {
      const settings = useSettingsStore();
      return this.kind === 'ipsc' ? settings.ipscPort : settings.mmdvmPort;
    },
    host(): string {
      return useSettingsStore().serverAddress || window.location.hostname;
    },
    details(): { label: string; value: string }[] {
      return [
        { label: 'Server address', value: this.host },
        { label: 'Port', value: String(this.port) },
        { label: this.kind === 'hotspot' ? 'Hotspot ID' : 'Repeater ID', value: String(this.repeater?.id ?? '') },
      ];
    },
    values(): ConnectValues {
      const r = this.repeater!;
      return {
        kind: this.kind!,
        host: this.host,
        port: this.port,
        id: r.id,
        password: this.secret || secretPlaceholder(this.kind!),
        callsign: r.owner?.callsign || '<callsign>',
        ownerId: r.owner?.id || '<DMR ID>',
        essid: essid(r.id),
      };
    },
    title(): string {
      const name = `${this.kind === 'hotspot' ? 'Hotspot' : 'Repeater'} ${this.repeater?.id}`;
      return this.firstVisit && !this.rotatedAt ? `${name} is enrolled` : `${name}: connect`;
    },
    lead(): string {
      const owner = this.repeater?.owner?.id ? `Owner ${this.repeater.owner.callsign} (${this.repeater.owner.id}). ` : '';
      if (this.firstVisit) {
        const where = this.kind === 'ipsc' ? 'CPS of the repeater' : `program that runs your ${this.noun}`;
        return `${owner}Put the values below in the ${where}.`;
      }
      return `${owner}You can open these steps at any time from the page of the ${this.noun}.`;
    },
    lastPing(): number {
      return timeOf(this.repeater?.last_ping_time);
    },
    connected(): boolean {
      if (!this.lastPing || this.now - this.lastPing >= SESSION_TIMEOUT_MS) return false;
      return !this.rotatedAt || this.lastPing > this.rotatedAt.getTime();
    },
    liveText(): string {
      if (this.connected) {
        const s = Math.max(0, Math.round((this.now - this.lastPing) / 1000));
        return `Connected now. Last ping ${s} ${s === 1 ? 'second' : 'seconds'} ago`;
      }
      if (this.rotatedAt) return `Waiting for a login with the new ${this.secretWord}`;
      if (this.lastPing) return `Not connected. Last ping ${formatDistanceStrict(this.lastPing, this.now)} ago`;
      return 'Waiting for the first login';
    },
    canRotate(): boolean {
      const user = useUserStore();
      if (!user.loggedIn || !this.repeater) return false;
      return user.admin || this.repeater.owner?.id === user.id;
    },
    next(): { label: string; to: string }[] {
      return [
        { label: 'Pick your talkgroups', to: '/repeaters' },
        { label: 'Check the connection', to: `/repeaters/${this.repeaterID}` },
        { label: 'Call the parrot', to: '/lastheard' },
      ];
    },
  },
  watch: {
    tabs(list: ClientTab[]) {
      if (!list.includes(this.tab as ClientTab)) this.tab = list[0] ?? '';
    },
  },
  methods: {
    fetchRepeater() {
      API.get(`/repeaters/${this.repeaterID}`)
        .then((res) => {
          this.repeater = res.data;
          if (this.repeater && !connectKind(this.repeater.type || 'mmdvm', this.repeater.id)) {
            this.error = 'This repeater does not connect over the network.';
          }
        })
        .catch((err) => {
          if (this.repeater) return;
          console.error(err);
          this.error = errorText(err, 'Could not load the repeater.');
        });
    },
    rotate() {
      this.rotating = true;
      API.post(`/repeaters/${this.repeaterID}/rotate-password`, {})
        .then((res) => {
          this.secret = res.data.password;
          this.rotatedAt = new Date(res.data.rotated_at);
          this.confirming = false;
        })
        .catch((err) => {
          console.error(err);
          toast.error('Error', { description: errorText(err, `Could not rotate the ${this.secretWord}. Try again later.`) });
        })
        .finally(() => {
          this.rotating = false;
        });
    },
  },
};
</script>

<style scoped>
.details {
  display: grid;
  grid-template-columns: minmax(150px, max-content) 1fr;
}

.details-k,
.details-v {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}

.details > div:nth-last-child(-n + 2) {
  border-bottom: 0;
}

.details-k {
  color: var(--muted-foreground);
  background: var(--stripe);
  font-weight: 500;
}

@media (max-width: 640px) {
  .details {
    grid-template-columns: 1fr;
  }

  .details-k {
    border-bottom: 0;
    padding-bottom: 2px;
    background: transparent;
  }

  .details > div:nth-last-child(2) {
    border-bottom: 0;
  }
}
</style>
