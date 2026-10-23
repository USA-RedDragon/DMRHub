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

    <Card v-if="state === 'denied'" data-test="admin-messages-denied">
      <CardHeader>
        <CardTitle>Admins only</CardTitle>
        <CardDescription>You need admin access to search all messages.</CardDescription>
      </CardHeader>
    </Card>

    <template v-else-if="state !== 'loading'">
      <p class="text-sm text-muted-foreground">
        Search every stored message, in every format, from every user. No cap and no opt-in filter.
      </p>

      <Card>
        <CardHeader>
          <CardTitle>Search</CardTitle>
        </CardHeader>
        <CardContent>
          <form
            class="grid grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] items-end gap-3"
            data-test="admin-search"
            @submit.prevent="search(1)"
          >
            <div class="flex min-w-0 flex-col gap-1">
              <label for="f-src" class="text-sm">From ID</label>
              <ShadInput id="f-src" v-model="filters.src" inputmode="numeric" placeholder="3191868" />
            </div>
            <div class="flex min-w-0 flex-col gap-1">
              <label for="f-dst" class="text-sm">To ID or talkgroup</label>
              <ShadInput id="f-dst" v-model="filters.dst" inputmode="numeric" placeholder="3140598 or 3100" />
            </div>
            <div class="flex min-w-0 flex-col gap-1">
              <label for="f-format" class="text-sm">Format</label>
              <ShadSelect v-model="formatChoice">
                <SelectTrigger id="f-format" class="w-full">
                  <SelectValue placeholder="Any format" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="any">Any format</SelectItem>
                  <SelectItem v-for="f in FORMAT_ORDER" :key="f" :value="f">{{ formatLabel(f) }}</SelectItem>
                </SelectContent>
              </ShadSelect>
            </div>
            <div class="flex min-w-0 flex-col gap-1">
              <label for="f-from" class="text-sm">From date</label>
              <ShadInput id="f-from" v-model="filters.from" type="date" />
            </div>
            <div class="flex min-w-0 flex-col gap-1">
              <label for="f-to" class="text-sm">To date</label>
              <ShadInput id="f-to" v-model="filters.to" type="date" />
            </div>
            <div class="flex min-w-0 flex-col gap-1">
              <label for="f-text" class="text-sm">Text contains</label>
              <ShadInput id="f-text" v-model="filters.text" placeholder="TEST" />
            </div>
            <div class="flex flex-wrap gap-2">
              <ShadButton type="submit">Search</ShadButton>
              <ShadButton type="button" variant="outline" @click="clear">Clear</ShadButton>
            </div>
          </form>
          <p v-if="searchError" class="mt-3 text-sm text-destructive" data-test="admin-search-error">
            {{ searchError }}
          </p>
        </CardContent>
      </Card>

      <MessageTable
        data-test="admin-table"
        admin
        :messages="rows"
        :loading="loading"
        :page="page"
        :page-size="pageSize"
        :page-count="Math.max(1, Math.ceil(total / pageSize))"
        :summary="summary"
        :selected-id="selected ? selected.id : null"
        empty-text="No messages match these filters"
        @update:page="search"
        @select="select"
      />

      <Card v-if="selected" data-test="admin-message-detail">
        <CardHeader>
          <CardTitle>Message {{ selected.id }}</CardTitle>
          <CardDescription>{{ longTime(selected.received_at) }}</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <dl class="flex flex-col gap-2 text-sm">
            <div v-for="row in detailRows" :key="row.label" class="flex items-baseline justify-between gap-4">
              <dt class="text-muted-foreground">{{ row.label }}</dt>
              <dd class="text-right font-medium tabular-nums">{{ row.value }}</dd>
            </div>
          </dl>
          <div>
            <div class="mb-1 text-sm">Raw (data header, then reassembled user data)</div>
            <pre class="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs" data-test="admin-raw">{{ hexDump(selected.raw) }}</pre>
          </div>
        </CardContent>
      </Card>
      <p v-else-if="detailError" class="text-sm text-destructive">{{ detailError }}</p>
      <p v-else class="text-sm text-muted-foreground">Select a row to see the raw message.</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useHead } from '@unhead/vue';
import MessageTable from '@/components/MessageTable.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import {
  Select as ShadSelect,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  FORMAT_ORDER,
  formatLabel,
  getAdminMessage,
  hexDump,
  sapLabel,
  searchAdminMessages,
  transportLabel,
  type AdminMessage,
  type AdminMessageDetail,
  type AdminMessageFilters,
} from '@/services/messages';
import { errorStatus, errorText } from '@/services/settings';

useHead({
  title: 'Messages',
  titleTemplate: 'Admin | %s | ' + (localStorage.getItem('title') || 'DMRHub'),
});

const pageSize = 25;

const emptyFilters = (): Required<AdminMessageFilters> => ({ src: '', dst: '', format: '', from: '', to: '', text: '' });

const state = ref<'loading' | 'ready' | 'denied'>('loading');
const filters = reactive(emptyFilters());
const applied = ref<AdminMessageFilters>(emptyFilters());
const rows = ref<AdminMessage[]>([]);
const total = ref(0);
const page = ref(1);
const loading = ref(false);
const searchError = ref('');
const selected = ref<AdminMessageDetail | null>(null);
const detailError = ref('');

const formatChoice = computed({
  get: () => filters.format || 'any',
  set: (value: string) => {
    filters.format = value === 'any' ? '' : value;
  },
});

const filtered = computed(() => Object.values(applied.value).some((v) => v));

const summary = computed(() =>
  `${rows.value.length} of ${total.value} ${filtered.value ? 'matching' : 'stored'} messages`,
);

const longTime = (value: string) =>
  new Date(value).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short',
  });

const detailRows = computed(() => {
  const m = selected.value;
  if (!m) {
    return [];
  }
  return [
    { label: 'SAP', value: sapLabel(m.sap) },
    { label: 'Blocks / octets', value: `${m.blocks} / ${m.octets}` },
    { label: 'Response', value: m.confirmed ? m.response.toUpperCase() : 'Unconfirmed' },
    { label: 'Checksum', value: m.checksum_ok ? 'OK' : 'Bad' },
    { label: 'Network / repeater / slot', value: `${transportLabel(m.transport)} / ${m.repeater_id} / TS${m.slot}` },
  ];
});

const search = async (nextPage = 1) => {
  searchError.value = '';
  if (nextPage === 1) {
    applied.value = { ...filters };
    selected.value = null;
    detailError.value = '';
  }
  loading.value = true;
  try {
    const res = await searchAdminMessages(applied.value, nextPage, pageSize);
    rows.value = res.messages;
    total.value = res.total;
    page.value = nextPage;
    state.value = 'ready';
  } catch (err) {
    const status = errorStatus(err);
    if (status === 401 || status === 403) {
      state.value = 'denied';
      return;
    }
    state.value = 'ready';
    searchError.value = errorText(err, 'The search failed.');
  } finally {
    loading.value = false;
  }
};

const clear = () => {
  Object.assign(filters, emptyFilters());
  search(1);
};

const select = async (id: number) => {
  detailError.value = '';
  try {
    selected.value = await getAdminMessage(id);
  } catch (err) {
    selected.value = null;
    detailError.value = errorText(err, `Could not load message ${id}.`);
  }
};

onMounted(() => search(1));
</script>
