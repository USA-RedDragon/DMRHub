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
  <Card class="mb-5" data-test="transcoding-card">
    <CardHeader class="flex flex-wrap items-center justify-between gap-2">
      <CardTitle>AMBE servers</CardTitle>
      <span class="flex flex-wrap gap-1.5"><SourceChips :setting="setting" /></span>
    </CardHeader>
    <CardContent>
      <p class="mb-3 text-sm text-muted-foreground">
        AMBEserver protocol, usually UDP port 2460. A call tries the servers in this order and holds one server for its
        whole length. Each server must report the decoder status of each frame; a server that cannot converts no call.
      </p>
      <div class="grid gap-2" data-test="ambe-servers">
        <div
          v-for="(row, i) in rows"
          :key="i"
          class="grid grid-cols-[minmax(0,1fr)_110px] items-center gap-2 md:grid-cols-[minmax(0,1fr)_110px_auto_auto]"
          :data-test="`ambe-server-${i}`"
        >
          <ShadInput
            :disabled="locked"
            :aria-label="`Address of server ${i + 1}`"
            placeholder="ambeserver.example.org:2460"
            :model-value="row.address"
            :aria-invalid="invalidAddress(row) ? true : undefined"
            :data-test="`ambe-server-address-${i}`"
            @update:model-value="(v: string | number) => setRow(i, { address: String(v) })"
          />
          <ShadInput
            :disabled="locked"
            aria-label="Calls at once"
            inputmode="numeric"
            placeholder="No limit"
            :model-value="callsText(row.calls)"
            :aria-invalid="invalidCalls(row) ? true : undefined"
            :data-test="`ambe-server-calls-${i}`"
            @update:model-value="(v: string | number) => setRow(i, { calls: String(v) })"
          />
          <span
            class="inline-flex items-center gap-1.5 justify-self-start rounded-full border px-2.5 text-xs leading-[22px] whitespace-nowrap"
            :data-test="`ambe-server-status-${i}`"
          >
            <span class="size-[7px] rounded-full" :class="dotOf(statusOf(row).tone)" />{{ statusOf(row).text }}
          </span>
          <ShadButton
            type="button"
            variant="outline"
            size="sm"
            :disabled="locked"
            :data-test="`ambe-server-remove-${i}`"
            @click="removeRow(i)"
          >Remove</ShadButton>
        </div>
      </div>
      <p class="mt-1.5 text-sm text-muted-foreground">
        Calls at once: empty is no limit. Use 1 for a server with one vocoder chip, which answers only its last client.
      </p>
      <div class="mt-2.5">
        <ShadButton type="button" variant="outline" size="sm" :disabled="locked" data-test="ambe-server-add" @click="addRow">
          Add a server
        </ShadButton>
      </div>
      <SettingNote path="dmr.transcoding.ambe-servers" menu class="mt-2" />
      <p v-if="error" class="mt-1 text-sm text-destructive-foreground" data-test="field-error">{{ error }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import SettingNote from '@/components/setup/SettingNote.vue';
import SourceChips from '@/components/setup/menu/SourceChips.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import {
  ambeServerError, callsError, callsText, serverStatus, statusText,
  type AmbeServerRow, type StatusText, type TranscodingStatus,
} from '@/lib/codec';
import { fieldLocked } from '@/lib/setupMenu';
import type { Setting } from '@/lib/setupState';
import API from '@/services/API';

const REFRESH_MS = 5000;

const props = defineProps<{ value: unknown; setting?: Setting; error?: string; saves: number }>();
const emit = defineEmits<{ (e: 'set', path: string, value: unknown): void }>();

const locked = computed(() => fieldLocked(props.setting));

const rows = computed<AmbeServerRow[]>(() => {
  if (!Array.isArray(props.value)) {
    return [];
  }
  return props.value.map((r) => {
    const row = (typeof r === 'object' && r !== null ? r : {}) as Partial<AmbeServerRow>;
    return { address: String(row.address ?? ''), calls: row.calls ?? 0 };
  });
});

const setRows = (next: AmbeServerRow[]) => emit('set', 'dmr.transcoding.ambe-servers', next);
const setRow = (i: number, change: Partial<AmbeServerRow>) => setRows(rows.value.map((r, j) => (j === i ? { ...r, ...change } : r)));
const addRow = () => setRows([...rows.value, { address: '', calls: 1 }]);
const removeRow = (i: number) => setRows(rows.value.filter((_, j) => j !== i));

const invalidAddress = (row: AmbeServerRow) => !!props.error && !!ambeServerError(row.address);
const invalidCalls = (row: AmbeServerRow) => !!props.error && !!callsError(row.calls);

const answer = ref<TranscodingStatus | null>(null);
const statusOf = (row: AmbeServerRow): StatusText => statusText(serverStatus(answer.value, row.address));
const dotOf = (tone: StatusText['tone']) => {
  switch (tone) {
    case 'ok':
      return 'bg-green-600 dark:bg-green-400';
    case 'bad':
      return 'bg-destructive-foreground';
    default:
      return 'bg-muted-foreground';
  }
};

let timer: ReturnType<typeof setInterval> | null = null;

const refresh = async () => {
  try {
    const res = await API.get('/config/transcoding');
    answer.value = res.data as TranscodingStatus;
  } catch {
    answer.value = null;
  }
};

onMounted(() => {
  refresh();
  timer = setInterval(refresh, REFRESH_MS);
});

onUnmounted(() => {
  if (timer !== null) {
    clearInterval(timer);
    timer = null;
  }
});

watch(() => props.saves, () => {
  refresh();
});
</script>
