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
  <div data-test="transcoding-card">
    <Card>
      <CardHeader>
        <CardTitle>Transcoding <SettingBadges path="dmr.transcoding.ambe-servers" /></CardTitle>
      </CardHeader>
      <CardContent>
        <p>
          Repeaters and hotspots that send Codec2 can talk with AMBE radios. DMRHub converts each call once, only for
          the targets that need the other codec. Codec2 is built in. AMBE needs an AMBE server, such as a hardware vocoder
          behind AMBEserver.
        </p>
        <Table class="mt-4" data-test="ambe-servers">
          <TableHeader>
            <TableRow>
              <TableHead>AMBE server</TableHead>
              <TableHead>Calls at once</TableHead>
              <TableHead v-if="showStatus">Status</TableHead>
              <TableHead><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="(row, i) in rows" :key="i" :data-test="`ambe-server-${i}`">
              <TableCell>
                <ShadInput
                  v-setting="locked"
                  aria-label="AMBE server"
                  placeholder="ambeserver.example.org:2460"
                  :model-value="row.address"
                  :aria-invalid="invalidAddress(row) ? true : undefined"
                  :data-test="`ambe-server-address-${i}`"
                  @update:model-value="(v) => setRow(i, { address: String(v) })"
                />
              </TableCell>
              <TableCell>
                <ShadInput
                  v-setting="locked"
                  aria-label="Calls at once"
                  inputmode="numeric"
                  placeholder="No limit"
                  class="w-28"
                  :model-value="callsText(row.calls)"
                  :aria-invalid="invalidCalls(row) ? true : undefined"
                  :data-test="`ambe-server-calls-${i}`"
                  @update:model-value="(v) => setRow(i, { calls: String(v) })"
                />
              </TableCell>
              <TableCell v-if="showStatus" :data-test="`ambe-server-status-${i}`">
                <template v-if="statusOf(row)">
                  <span v-if="statusOf(row)!.tone === 'none'" class="text-sm text-muted-foreground">
                    {{ statusOf(row)!.text }}
                  </span>
                  <div v-else class="flex items-center gap-2 text-sm">
                    <span
                      class="size-2 shrink-0 rounded-full"
                      :class="statusOf(row)!.tone === 'ok' ? 'bg-green-500' : 'bg-red-500'"
                    />
                    <span>{{ statusOf(row)!.text }}</span>
                  </div>
                </template>
              </TableCell>
              <TableCell>
                <ShadButton
                  type="button"
                  variant="outline"
                  size="sm"
                  :disabled="locked"
                  :data-test="`ambe-server-remove-${i}`"
                  @click="removeRow(i)"
                >Remove</ShadButton>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <div class="mt-3">
          <ShadButton type="button" variant="outline" size="sm" :disabled="locked" data-test="ambe-server-add" @click="addRow">
            Add AMBE server
          </ShadButton>
        </div>
        <p class="mt-3 text-sm text-muted-foreground">
          Host and UDP port of each server. Calls at once: leave it empty when the server has no limit; use 1 for an
          AMBE server with one chip, which answers only its last client. Each converted call uses one server for its whole length. When every server is
          busy, a call passes through in its own codec. With no server, transcoding is off.
        </p>
        <SettingNote path="dmr.transcoding.ambe-servers" />
        <span v-if="errors?.['ambe-servers']" class="p-error">{{ errors['ambe-servers'] }}</span>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, inject, onMounted, onUnmounted, ref, watch } from 'vue';

import SettingBadges from '@/components/setup/SettingBadges.vue';
import SettingNote from '@/components/setup/SettingNote.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
  ambeServerError,
  callsError,
  callsText,
  serverStatus,
  statusText,
  type AmbeServerRow,
  type StatusText,
  type TranscodingStatus,
} from '@/lib/codec';
import { setupStateKey, settingLocked } from '@/lib/setupState';
import { settingDirective as vSetting } from '@/lib/settingDirective';
import API from '@/services/API';

const REFRESH_MS = 5000;

const setupState = inject(setupStateKey, null);
const locked = computed(() => settingLocked(setupState, 'dmr.transcoding.ambe-servers'));

const props = withDefaults(defineProps<{
  modelValue: Record<string, unknown>;
  errors?: Record<string, string>;
  showStatus?: boolean;
  saves?: number;
}>(), {
  errors: undefined,
  showStatus: false,
  saves: 0,
});

const emit = defineEmits<{
  (e: 'update:modelValue', value: Record<string, unknown>): void
}>();

const rows = computed<AmbeServerRow[]>(() => {
  const list = props.modelValue?.['ambe-servers'];
  if (!Array.isArray(list)) {
    return [];
  }
  return list.map((r) => {
    const row = (typeof r === 'object' && r !== null ? r : {}) as Partial<AmbeServerRow>;
    return { address: String(row.address ?? ''), calls: row.calls ?? 0 };
  });
});

const setRows = (next: AmbeServerRow[]) => {
  emit('update:modelValue', { ...props.modelValue, 'ambe-servers': next });
};

const setRow = (i: number, change: Partial<AmbeServerRow>) => {
  setRows(rows.value.map((r, j) => (j === i ? { ...r, ...change } : r)));
};

const addRow = () => {
  setRows([...rows.value, { address: '', calls: 0 }]);
};

const removeRow = (i: number) => {
  setRows(rows.value.filter((_, j) => j !== i));
};

const invalidAddress = (row: AmbeServerRow) => !!props.errors?.['ambe-servers'] && !!ambeServerError(row.address);
const invalidCalls = (row: AmbeServerRow) => !!props.errors?.['ambe-servers'] && !!callsError(row.calls);

const answer = ref<TranscodingStatus | null>(null);
const statusOf = (row: AmbeServerRow): StatusText | null =>
  answer.value === null ? null : statusText(serverStatus(answer.value, row.address));
let timer: ReturnType<typeof setInterval> | null = null;

const refresh = async () => {
  if (!props.showStatus) {
    return;
  }
  try {
    const res = await API.get('/config/transcoding');
    answer.value = res.data as TranscodingStatus;
  } catch {
    answer.value = null;
  }
};

onMounted(() => {
  if (props.showStatus) {
    refresh();
    timer = setInterval(refresh, REFRESH_MS);
  }
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
