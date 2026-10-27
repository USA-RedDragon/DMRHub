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
  <div data-test="modem-test">
    <p class="text-sm text-muted-foreground">
      Opens the serial port with the settings above and asks the board for its firmware, without saving. While the modem
      runs, the test shows its live state instead.
    </p>
    <div class="mt-2.5">
      <ShadButton type="button" variant="outline" size="sm" :disabled="running" data-test="modem-test-run" @click="run">
        {{ running ? 'Testing the modem' : 'Test the modem' }}
      </ShadButton>
    </div>
    <div
      v-if="view"
      class="mt-3 rounded-lg border border-l-[3px] px-3.5 py-3 text-sm"
      :class="view.tone === 'ok' ? 'border-l-green-600 dark:border-l-green-400' : 'border-l-destructive-foreground'"
      role="status"
      data-test="modem-test-result"
    >
      <b class="flex items-center gap-1.5 font-semibold">
        <span
          class="size-[7px] rounded-full"
          :class="view.tone === 'ok' ? 'bg-green-600 dark:bg-green-400' : 'bg-destructive-foreground'"
        />{{ view.title }}
      </b>
      <dl v-if="view.rows.length > 0" class="mt-2 grid grid-cols-[max-content_1fr] gap-x-3.5 gap-y-0.5">
        <template v-for="[k, v] in view.rows" :key="k">
          <dt class="text-muted-foreground">{{ k }}</dt>
          <dd class="break-words">{{ v }}</dd>
        </template>
      </dl>
      <p v-if="view.text" class="mt-1.5 text-muted-foreground">{{ view.text }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { testView, type ModemTest, type ResultView } from '@/lib/modem';
import { getAt, TABS, tabFields, type Read } from '@/lib/setupMenu';
import API from '@/services/API';

const props = defineProps<{ read: Read }>();

const running = ref(false);
const view = ref<ResultView | null>(null);

const asRecord = (v: unknown): Record<string, unknown> | undefined =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined;

const modemTab = TABS.find((t) => t.id === 'modem');
const labels: Record<string, string> = Object.fromEntries((modemTab ? tabFields(modemTab) : []).map((f) => [f.path, f.label]));

const errorList = (errors: unknown): string => {
  const modem = asRecord(getAt(errors, 'dmr.modem')) ?? {};
  const out: string[] = [];
  const walk = (v: unknown, path: string) => {
    if (typeof v === 'string') {
      out.push(`${labels[path] ?? path}: ${v}`);
      return;
    }
    for (const [k, e] of Object.entries(asRecord(v) ?? {})) {
      walk(e, `${path}.${k}`);
    }
  };
  walk(modem, 'dmr.modem');
  return out.join(' ');
};

const run = async () => {
  running.value = true;
  view.value = null;
  try {
    const res = await API.post('/config/modem/test', props.read('dmr.modem') ?? {});
    view.value = testView(res.data as ModemTest);
  } catch (error: unknown) {
    const res = asRecord(asRecord(error)?.response);
    const data = asRecord(res?.data);
    if (res?.status === 400 && data?.errors) {
      view.value = { tone: 'bad', title: 'Fix these settings first', rows: [], text: errorList(data.errors) };
    } else if (res?.status === 409) {
      view.value = { tone: 'bad', title: 'Another test runs now', rows: [], text: 'Try again in a few seconds.' };
    } else {
      view.value = { tone: 'bad', title: 'The test did not run', rows: [], text: typeof data?.error === 'string' ? data.error : undefined };
    }
  } finally {
    running.value = false;
  }
};
</script>
