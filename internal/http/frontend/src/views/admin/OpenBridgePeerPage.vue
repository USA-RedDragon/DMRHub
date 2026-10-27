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

    <template v-else-if="peer">
      <div>
        <div class="mb-1.5 text-[13px] text-muted-foreground" data-test="crumb">
          <template v-if="isAdmin">Admin / </template><RouterLink :to="listPath">Peers</RouterLink> /
          <span>{{ peer.id }}</span>
        </div>
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="min-w-0 flex-[1_1_20rem]">
            <h1 class="mb-1.5 text-[26px] leading-tight font-bold" data-test="title">{{ title }}</h1>
            <p v-if="peer.owner?.id" class="text-muted-foreground" data-test="lead">
              Owner {{ peer.owner.callsign }} ({{ peer.owner.id }}).
            </p>
          </div>
          <span
            class="mt-1.5 inline-flex items-center gap-1.5 rounded-full border px-2.5 text-xs leading-[22px] font-medium whitespace-nowrap"
            :class="pingRecent ? 'border-success/55' : ''"
            data-test="ping"
          >
            <span class="size-[7px] shrink-0 rounded-full" :class="pingRecent ? 'bg-success' : 'bg-muted-foreground'" />
            <span>{{ pingText }}</span>
          </span>
        </div>
      </div>

      <Card class="gap-5">
        <CardHeader><CardTitle>Peer</CardTitle></CardHeader>
        <CardContent>
          <div class="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-x-[18px] gap-y-3.5">
            <div>
              <label class="mb-1 block font-medium" for="owner">Owner</label>
              <ShadInput id="owner" type="text" :model-value="ownerText" readonly class="bg-muted text-muted-foreground" />
            </div>
            <div>
              <label class="mb-1 block font-medium" for="pid">Peer ID</label>
              <ShadInput id="pid" type="text" :model-value="String(peer.id)" readonly class="bg-muted text-muted-foreground" />
            </div>
            <div>
              <label class="mb-1 block font-medium" for="ip">IP Address</label>
              <ShadInput id="ip" v-model="form.ip" type="text" />
            </div>
            <div>
              <label class="mb-1 block font-medium" for="port">Port</label>
              <ShadInput id="port" v-model="form.port" type="text" inputmode="numeric" />
            </div>
          </div>
          <div class="mt-4 flex flex-col gap-2">
            <label class="check"><input id="ingress" v-model="form.ingress" type="checkbox" />Receive DMR traffic from this peer</label>
            <label class="check"><input id="egress" v-model="form.egress" type="checkbox" />Transmit DMR traffic to this peer</label>
            <label class="check">
              <input id="enhanced" v-model="form.enhanced" type="checkbox" :disabled="!isAdmin" />Use FreeDMR enhanced
              OpenBridge with this peer
            </label>
            <label class="check">
              <input id="codec2" v-model="form.codec2" type="checkbox" :disabled="!isAdmin" />This peer carries Codec2
            </label>
          </div>
        </CardContent>
        <CardFooter class="justify-end gap-2">
          <ShadButton
            type="button"
            variant="outline"
            size="sm"
            class="border-destructive-foreground/50 text-destructive-foreground"
            data-test="delete"
            @click="deleting = true"
          >Delete</ShadButton>
          <ShadButton type="button" size="sm" :disabled="saving" data-test="save" @click="save">Save</ShadButton>
        </CardFooter>
      </Card>

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
            <div class="details-k">Password</div>
            <div v-if="secret" class="details-v justify-between">
              <span
                class="rounded-md border bg-muted px-2.5 py-1 font-mono text-xl font-semibold tracking-[0.04em] break-all"
                data-test="secret"
              >{{ secret }}</span>
              <CopyButton :text="secret" label="Copy password" />
            </div>
            <div v-else class="details-v justify-between">
              <span class="text-muted-foreground" data-test="secret-hidden">Hidden</span>
              <ShadButton
                type="button"
                variant="outline"
                size="sm"
                class="h-7 border-destructive-foreground/50 px-2.5 text-[13px] text-destructive-foreground"
                data-test="rotate"
                @click="confirming = true"
              >Rotate password</ShadButton>
            </div>
          </div>
          <template v-if="secret">
            <div class="mt-3.5 rounded-[10px] border border-warn-border bg-warn-bg px-3.5 py-3 text-sm text-warn-fg" data-test="copy-now">
              <b>Copy the password now.</b> DMRHub does not show it again. If you lose it, open this peer and click
              <b>Rotate password</b>.
            </div>
            <div v-if="rotatedAt" class="mt-3.5 rounded-[10px] border bg-muted px-3.5 py-3 text-sm" data-test="rotated">
              The old password stopped working.
            </div>
          </template>
        </CardContent>
      </Card>

      <PeerRules
        :peer-id="peer.id"
        :ingress="form.ingress"
        :egress="form.egress"
        :ingress-private="peer.ingress_private ?? false"
        :egress-private="peer.egress_private ?? false"
        @private-changed="privateChanged"
      />

      <Card class="gap-5 pb-5">
        <CardHeader><CardTitle data-test="setup-title">{{ setupTitle }}</CardTitle></CardHeader>
        <Tabs v-model="tab" class="gap-0">
          <TabsList
            v-if="tabs.length > 1"
            class="h-auto w-full justify-start gap-0.5 overflow-x-auto rounded-none border-b bg-transparent p-0 px-6"
          >
            <TabsTrigger
              v-for="t in tabs"
              :key="t"
              :value="t"
              class="-mb-px h-auto flex-none rounded-none border-0 border-b-2 border-transparent px-3 py-2 font-semibold text-muted-foreground shadow-none data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:text-foreground data-[state=active]:shadow-none dark:text-muted-foreground dark:data-[state=active]:border-primary dark:data-[state=active]:bg-transparent"
              :data-test="`tab-${t}`"
            >
              {{ tabInfo[t].name }}
              <span v-if="tabInfo[t].tested === true" class="size-1.5 rounded-full bg-success" title="Tested" />
            </TabsTrigger>
          </TabsList>
          <TabsContent
            v-for="t in tabs"
            :key="t"
            :value="t"
            class="px-6"
            :class="tabs.length > 1 ? 'pt-5' : ''"
            :data-test="`panel-${t}`"
          >
            <PeerSteps :tab="t" :values="values" />
          </TabsContent>
        </Tabs>
      </Card>

      <Card class="gap-5">
        <CardHeader><CardTitle>Next</CardTitle></CardHeader>
        <CardContent class="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3">
          <RouterLink
            :to="listPath"
            class="no-underline block rounded-[10px] border px-3.5 py-3 font-bold text-inherit hover:bg-accent"
          >Back to Peers</RouterLink>
        </CardContent>
      </Card>

      <ShadDialog :open="confirming" @update:open="confirming = $event">
        <DialogContent data-test="rotate-dialog">
          <DialogTitle>Rotate the password of {{ peer.id }}?</DialogTitle>
          <DialogDescription class="text-foreground">
            <b>The link stops at once.</b> It stays down until you put the new password in the other server.
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
            >Rotate password</ShadButton>
          </div>
        </DialogContent>
      </ShadDialog>

      <ShadDialog :open="deleting" @update:open="deleting = $event">
        <DialogContent data-test="delete-dialog">
          <DialogTitle>Delete peer {{ peer.id }}?</DialogTitle>
          <DialogDescription class="text-foreground">
            <b>The link stops at once.</b> DMRHub deletes the peer and its rules.
          </DialogDescription>
          <div class="flex flex-wrap justify-end gap-2">
            <ShadButton type="button" variant="outline" size="sm" data-test="delete-cancel" @click="deleting = false">
              Cancel
            </ShadButton>
            <ShadButton
              type="button"
              variant="outline"
              size="sm"
              class="border-destructive-foreground/50 text-destructive-foreground"
              data-test="delete-confirm"
              @click="remove"
            >Delete</ShadButton>
          </div>
        </DialogContent>
      </ShadDialog>
    </template>
  </div>
</template>

<script lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog as ShadDialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input as ShadInput } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { toast } from '@/components/ui/toast';
import CopyButton from '@/components/enroll/CopyButton.vue';
import PeerRules from '@/components/peer/PeerRules.vue';
import PeerSteps from '@/components/peer/PeerSteps.vue';
import {
  PEER_PASSWORD_PLACEHOLDER,
  PEER_TABS,
  peerTabs,
  takePeerPassword,
  type PeerTab,
  type PeerValues,
} from '@/lib/peer';
import API from '@/services/API';
import { errorText } from '@/services/settings';
import { useSettingsStore, useUserStore } from '@/store';
import { formatDistanceStrict } from 'date-fns';
import { RouterLink } from 'vue-router';

type PeerData = {
  id: number;
  last_ping_time?: string;
  ip: string;
  port: number;
  ingress: boolean;
  egress: boolean;
  ingress_private?: boolean;
  egress_private?: boolean;
  enhanced: boolean;
  codec2: boolean;
  owner?: { id: number; callsign: string };
};

/** PING_RECENT_MS is how long after its last ping a peer counts as sending; DMRHub records the ping of a sending peer every 30 s. */
const PING_RECENT_MS = 75_000;

/** POLL_MS is how often the page reads the last ping of the peer again. */
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
    CardFooter,
    CardHeader,
    CardTitle,
    CopyButton,
    DialogContent,
    DialogDescription,
    DialogTitle,
    PeerRules,
    PeerSteps,
    RouterLink,
    ShadButton,
    ShadDialog,
    ShadInput,
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
  },
  head: {
    title: 'OpenBridge Peer',
  },
  data() {
    return {
      peerID: 0,
      peer: null as PeerData | null,
      form: { ip: '', port: '', ingress: false, egress: false, enhanced: false, codec2: false },
      error: '',
      secret: '',
      firstVisit: false,
      rotatedAt: null as Date | null,
      confirming: false,
      rotating: false,
      deleting: false,
      saving: false,
      tab: '' as PeerTab | '',
      tabInfo: PEER_TABS,
      now: Date.now(),
      poll: null as ReturnType<typeof setInterval> | null,
      clock: null as ReturnType<typeof setInterval> | null,
    };
  },
  mounted() {
    this.peerID = Number(this.$route.params.id);
    const kept = takePeerPassword(this.peerID);
    if (kept) {
      this.secret = kept;
      this.firstVisit = true;
    }
    useSettingsStore().fetchConfig();
    this.fetchPeer();
    this.poll = setInterval(() => this.fetchPing(), POLL_MS);
    this.clock = setInterval(() => {
      this.now = Date.now();
    }, 1000);
  },
  unmounted() {
    if (this.poll !== null) clearInterval(this.poll);
    if (this.clock !== null) clearInterval(this.clock);
  },
  computed: {
    isAdmin(): boolean {
      return useUserStore().admin;
    },
    listPath(): string {
      return this.isAdmin ? '/admin/peers' : '/peers';
    },
    ownerText(): string {
      const o = this.peer?.owner;
      return o?.id ? `${o.id} - ${o.callsign}` : '';
    },
    host(): string {
      return useSettingsStore().serverAddress || window.location.hostname;
    },
    port(): number {
      return useSettingsStore().openBridgePort;
    },
    details(): { label: string; value: string }[] {
      return [
        { label: 'Server address', value: this.host },
        { label: 'Port', value: String(this.port) },
        { label: 'Peer ID', value: String(this.peer?.id ?? '') },
      ];
    },
    tabs(): PeerTab[] {
      return peerTabs(this.form.enhanced);
    },
    setupTitle(): string {
      const first = this.tabs[0];
      return this.tabs.length === 1 && first ? `Set up ${PEER_TABS[first].name}` : 'Set up the other server';
    },
    values(): PeerValues {
      return {
        host: this.host,
        port: this.port,
        id: this.peer?.id ?? 0,
        password: this.secret || PEER_PASSWORD_PLACEHOLDER,
        ip: this.form.ip,
        peerPort: this.form.port,
      };
    },
    lastPing(): number {
      return timeOf(this.peer?.last_ping_time);
    },
    pingRecent(): boolean {
      return this.lastPing > 0 && this.now - this.lastPing < PING_RECENT_MS;
    },
    pingText(): string {
      if (!this.lastPing) return 'Last ping: never';
      return `Last ping ${formatDistanceStrict(this.lastPing, Math.max(this.now, this.lastPing))} ago`;
    },
    title(): string {
      return this.firstVisit && !this.rotatedAt ? `Peer ${this.peer?.id} is added` : `Peer ${this.peer?.id}`;
    },
  },
  watch: {
    tabs: {
      immediate: true,
      handler(list: PeerTab[]) {
        if (!list.includes(this.tab as PeerTab)) this.tab = list[0] ?? '';
      },
    },
  },
  methods: {
    load(p: PeerData) {
      this.peer = p;
      this.form = {
        ip: p.ip,
        port: String(p.port),
        ingress: p.ingress,
        egress: p.egress,
        enhanced: p.enhanced,
        codec2: p.codec2,
      };
    },
    privateChanged(e: { ingress: boolean; on: boolean }) {
      if (!this.peer) return;
      if (e.ingress) this.peer.ingress_private = e.on;
      else this.peer.egress_private = e.on;
    },
    fetchPeer() {
      API.get(`/peers/${this.peerID}`)
        .then((res) => this.load(res.data))
        .catch((err) => {
          console.error(err);
          this.error = errorText(err, 'Could not load the peer.');
        });
    },
    fetchPing() {
      API.get(`/peers/${this.peerID}`)
        .then((res) => {
          if (this.peer) this.peer.last_ping_time = res.data.last_ping_time;
        })
        .catch((err) => console.error(err));
    },
    save() {
      const port = Number(this.form.port);
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        toast.error('Error', { description: 'Port must be between 1 and 65535' });
        return;
      }
      const body: Record<string, unknown> = {
        ip: this.form.ip.trim(),
        port,
        ingress: this.form.ingress,
        egress: this.form.egress,
      };
      if (this.isAdmin) {
        body.enhanced = this.form.enhanced;
        body.codec2 = this.form.codec2;
      }
      this.saving = true;
      API.patch(`/peers/${this.peerID}`, body)
        .then((res) => {
          this.load(res.data);
          toast.success('Saved', { description: `Peer ${this.peerID} saved` });
        })
        .catch((err) => {
          console.error(err);
          toast.error('Error', { description: errorText(err, `Could not save peer ${this.peerID}.`) });
        })
        .finally(() => {
          this.saving = false;
        });
    },
    remove() {
      API.delete(`/peers/${this.peerID}`)
        .then(() => {
          this.deleting = false;
          toast.success('Deleted', { description: `Peer ${this.peerID} deleted` });
          this.$router.push(this.listPath);
        })
        .catch((err) => {
          console.error(err);
          toast.error('Error', { description: errorText(err, `Could not delete peer ${this.peerID}.`) });
        });
    },
    rotate() {
      this.rotating = true;
      API.post(`/peers/${this.peerID}/rotate-password`, {})
        .then((res) => {
          this.secret = res.data.password;
          this.rotatedAt = new Date(res.data.rotated_at);
          this.confirming = false;
        })
        .catch((err) => {
          console.error(err);
          toast.error('Error', { description: errorText(err, 'Could not rotate the password. Try again later.') });
        })
        .finally(() => {
          this.rotating = false;
        });
    },
  },
};
</script>

<style scoped>
.check {
  display: flex;
  align-items: center;
  gap: 8px;
}

.check input {
  width: 16px;
  height: 16px;
  margin: 0;
  accent-color: var(--primary);
}

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
