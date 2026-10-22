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
  <div class="flex flex-col gap-4">
    <h1>Messages</h1>

    <Card v-if="state === 'anon'" data-test="messages-anon">
      <CardHeader>
        <CardTitle>Log in to see your messages</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <p class="text-sm">Messages are private to the owner of each radio ID.</p>
        <div>
          <ShadButton as-child>
            <RouterLink to="/login">Login</RouterLink>
          </ShadButton>
        </div>
      </CardContent>
    </Card>

    <Card v-else-if="state === 'off'" data-test="messages-off">
      <CardHeader>
        <CardTitle>SMS Storage is off</CardTitle>
        <CardDescription>
          Turn on SMS Storage in Settings to see the text messages sent to and from your radio IDs.
        </CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <p class="max-w-[70ch] border-l-[3px] border-border py-1 pl-3.5 text-sm" data-test="sms-privacy">
          {{ privacyText }}
        </p>
        <div>
          <ShadButton as-child>
            <RouterLink to="/settings">Go to Settings</RouterLink>
          </ShadButton>
        </div>
      </CardContent>
    </Card>

    <template v-else-if="state === 'on'">
      <p class="text-sm text-muted-foreground" data-test="messages-ids">
        Text messages to and from your radio IDs ({{ radioIDList }}).
        <RouterLink to="/settings">Change in Settings</RouterLink>
      </p>

      <Tabs v-model="box" class="gap-4">
        <TabsList class="self-start">
          <TabsTrigger value="inbox" class="data-[state=inactive]:text-muted-foreground" data-test="tab-inbox">
            Inbox<span class="text-xs text-muted-foreground">{{ boxes.inbox.count }}</span>
          </TabsTrigger>
          <TabsTrigger value="outbox" class="data-[state=inactive]:text-muted-foreground" data-test="tab-outbox">
            Outbox<span class="text-xs text-muted-foreground">{{ boxes.outbox.count }}</span>
          </TabsTrigger>
        </TabsList>
        <TabsContent v-for="name in boxNames" :key="name" :value="name" class="flex flex-col gap-4">
          <p class="text-sm text-muted-foreground">
            {{ boxDescriptions[name] }} Only the {{ cap }} most recent messages are shown.
          </p>
          <MessageTable
            :data-test="`table-${name}`"
            :messages="boxes[name].rows"
            :loading="boxes[name].loading"
            :page="boxes[name].page"
            :page-size="pageSize"
            :page-count="boxPageCount(boxes[name].total, pageSize)"
            :summary="`showing ${boxes[name].rows.length} · this box shows the ${cap} most recent messages`"
            @update:page="(page: number) => loadBox(name, page)"
          />
        </TabsContent>
      </Tabs>

      <Card v-if="stats" data-test="message-stats">
        <CardHeader>
          <CardTitle>Message Stats</CardTitle>
          <CardDescription>
            Counts cover only the messages in your inbox and outbox above, so at most {{ cap }} each.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-4">
            <div v-for="tile in statTiles" :key="tile.label" :data-test="`stat-${tile.key}`">
              <div class="text-2xl font-semibold leading-tight tabular-nums">{{ tile.value }}</div>
              <div class="text-[0.8125rem] text-muted-foreground">{{ tile.label }}</div>
            </div>
          </div>
          <div class="grid gap-4 min-[900px]:grid-cols-2">
            <dl class="flex flex-col gap-2 text-sm" data-test="stats-by-format">
              <div class="flex items-baseline justify-between gap-4">
                <dt class="font-semibold">By format</dt>
                <dd></dd>
              </div>
              <div v-for="row in formatRows" :key="row.label" class="flex items-baseline justify-between gap-4">
                <dt class="text-muted-foreground">{{ row.label }}</dt>
                <dd class="font-medium tabular-nums">{{ row.count }}</dd>
              </div>
            </dl>
            <dl class="flex flex-col gap-2 text-sm" data-test="stats-by-day">
              <div class="flex items-baseline justify-between gap-4">
                <dt class="font-semibold">By day (last 7 days)</dt>
                <dd></dd>
              </div>
              <div v-for="row in dayRows" :key="row.day" class="flex items-baseline justify-between gap-4">
                <dt class="text-muted-foreground">{{ row.label }}</dt>
                <dd class="font-medium tabular-nums">{{ row.count }}</dd>
              </div>
            </dl>
          </div>
        </CardContent>
      </Card>
    </template>

    <Card v-else-if="state === 'error'">
      <CardHeader>
        <CardTitle>Error</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-destructive">Could not load your messages.</p>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { useHead } from '@unhead/vue';
import MessageTable from '@/components/MessageTable.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  FORMAT_ORDER,
  MESSAGE_BOX_CAP,
  applyMessageEvent,
  boxPageCount,
  formatLabel,
  getBox,
  getStats,
  type Message,
  type MessageBox,
  type MessageEvent,
  type MessageStats,
} from '@/services/messages';
import { errorStatus, getRadioIDs, getSettings } from '@/services/settings';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

useHead({ title: 'Messages' });

const privacyText =
  'When SMS Storage is on, the text messages sent to and from the radio IDs that you own appear on your ' +
  'Messages page, up to the 250 newest in each box.';

const cap = MESSAGE_BOX_CAP;
const pageSize = 25;
const boxNames: MessageBox[] = ['inbox', 'outbox'];
const boxDescriptions: Record<MessageBox, string> = {
  inbox: 'Private messages sent to your radio IDs. Group messages to talkgroups are not shown here.',
  outbox: 'Messages sent from your radio IDs, including group messages to talkgroups.',
};

type BoxState = {
  rows: Message[];
  total: number;
  page: number;
  loading: boolean;
  count: number;
  newestId: number;
  request: number;
};

const newBox = (): BoxState => ({ rows: [], total: 0, page: 1, loading: false, count: 0, newestId: 0, request: 0 });

const state = ref<'loading' | 'anon' | 'off' | 'on' | 'error'>('loading');
const box = ref<MessageBox>('inbox');
const boxes = reactive<Record<MessageBox, BoxState>>({ inbox: newBox(), outbox: newBox() });
const radioIDs = ref<number[]>([]);
const stats = ref<MessageStats | null>(null);
let socket: { close(): void } | null = null;
let statsTimer: ReturnType<typeof setTimeout> | null = null;
let statsRequest = 0;
let alive = true;

const radioIDList = computed(() => radioIDs.value.join(', '));

const statTiles = computed(() => {
  const s = stats.value;
  return [
    { key: 'inbox', label: 'Inbox', value: Math.min(s?.inbox ?? 0, cap) },
    { key: 'outbox', label: 'Outbox', value: Math.min(s?.outbox ?? 0, cap) },
    { key: 'ack', label: 'Delivered (ACK)', value: s?.ack ?? 0 },
    { key: 'nack', label: 'Rejected (NACK)', value: s?.nack ?? 0 },
  ];
});

const formatRows = computed(() => {
  const byFormat = stats.value?.by_format ?? {};
  const rows = FORMAT_ORDER.filter((f) => (byFormat[f] ?? 0) > 0).map((f) => ({
    label: formatLabel(f),
    count: byFormat[f] ?? 0,
  }));
  return rows.length > 0 ? rows : [{ label: 'None', count: 0 }];
});

const dayLabel = (day: string) => {
  const [y, m, d] = day.split('-').map(Number);
  if (!y || !m || !d) {
    return day;
  }
  return new Date(y, m - 1, d).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
};

const dayRows = computed(() =>
  (stats.value?.by_day ?? []).map((row) => ({ day: row.day, label: dayLabel(row.day), count: row.count })),
);

const loadBox = async (name: MessageBox, page = 1) => {
  const b = boxes[name];
  const request = ++b.request;
  b.loading = true;
  try {
    const res = await getBox(name, page, pageSize);
    if (request !== b.request) {
      return;
    }
    b.rows = res.messages;
    b.total = res.total;
    b.page = page;
    if (page === 1) {
      b.newestId = Math.max(b.newestId, ...res.messages.map((m) => m.id));
    }
  } catch (err) {
    console.error(err);
  } finally {
    if (request === b.request) {
      b.loading = false;
    }
  }
};

const fetchStats = async (): Promise<MessageStats> => {
  try {
    return await getStats();
  } catch (err) {
    if (errorStatus(err) === 400) {
      return getStats('UTC');
    }
    throw err;
  }
};

const loadStats = async () => {
  const request = ++statsRequest;
  try {
    const s = await fetchStats();
    if (request !== statsRequest) {
      return;
    }
    stats.value = s;
    boxes.inbox.count = Math.min(s.inbox, cap);
    boxes.outbox.count = Math.min(s.outbox, cap);
  } catch (err) {
    console.error(err);
  }
};

const scheduleStats = () => {
  if (statsTimer !== null) {
    return;
  }
  statsTimer = setTimeout(() => {
    statsTimer = null;
    loadStats();
  }, 1000);
};

const onSocketMessage = (event: { data: string }) => {
  let parsed: MessageEvent;
  try {
    parsed = JSON.parse(event.data);
  } catch {
    return;
  }
  if (!parsed || !parsed.message || !Array.isArray(parsed.boxes)) {
    return;
  }
  for (const name of boxNames) {
    const b = boxes[name];
    const res = applyMessageEvent(b.rows, parsed, name, b.page, pageSize, b.newestId);
    b.rows = res.rows;
    if (res.added) {
      b.newestId = Math.max(b.newestId, parsed.message.id);
      b.total = Math.min(b.total + 1, cap);
    }
  }
  scheduleStats();
};

const load = async () => {
  try {
    const [settings, ids] = await Promise.all([getSettings(), getRadioIDs()]);
    radioIDs.value = ids.map((rid) => rid.id);
    if (!settings.sms_storage) {
      state.value = 'off';
      return;
    }
    state.value = 'on';
  } catch (err) {
    state.value = errorStatus(err) === 401 ? 'anon' : 'error';
    return;
  }
  await Promise.all([loadBox('inbox'), loadBox('outbox'), loadStats()]);
  if (!alive) {
    return;
  }
  socket = ws.connect(getWebsocketURI() + '/messages', onSocketMessage);
};

onMounted(load);

onUnmounted(() => {
  alive = false;
  if (statsTimer !== null) {
    clearTimeout(statsTimer);
  }
  if (socket) {
    socket.close();
  }
});
</script>
