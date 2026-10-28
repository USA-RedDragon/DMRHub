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
  <Card data-test="talkgroup-access">
    <CardHeader>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <CardTitle id="tg-access-label">Who can link</CardTitle>
        <SegmentPicker
          v-if="loaded"
          name="tg-access"
          labelled-by="tg-access-label"
          :options="modeOptions"
          :model-value="draft.mode"
          @update:model-value="setMode"
        />
      </div>
    </CardHeader>
    <CardContent v-if="loaded" class="flex flex-col gap-4">
      <template v-if="draft.mode === 'approved'">
        <div class="flex flex-wrap gap-1.5" data-test="access-repeaters">
          <span v-for="r in draft.repeaters" :key="r.id" class="chip">
            <span class="font-mono">{{ r.id }}</span>
            <span>{{ r.callsign }}</span>
            <button type="button" class="chip-x" :aria-label="`Remove ${r.id}`" @click="removeRepeater(r.id)">&times;</button>
          </span>
        </div>
        <form class="flex flex-wrap items-center gap-2" novalidate @submit.prevent="addRepeater">
          <ShadInput
            v-model="query"
            class="h-8 min-w-0 flex-[1_1_9rem] font-mono"
            placeholder="Repeater ID"
            aria-label="Add repeater"
            :aria-invalid="!!addError"
            data-test="add-repeater"
          />
          <ShadButton type="submit" variant="outline" size="sm">Add</ShadButton>
          <p v-if="addError" class="w-full text-sm text-destructive">{{ addError }}</p>
        </form>
      </template>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <span v-if="message" class="mr-auto text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'" data-test="access-message">{{ message }}</span>
        <ShadButton type="button" variant="outline" :disabled="busy" data-test="access-reset" @click="reset">Reset</ShadButton>
        <ShadButton type="button" :disabled="busy" data-test="access-save" @click="save">Save</ShadButton>
      </div>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import SegmentPicker from '@/components/SegmentPicker.vue';
import API from '@/services/API';
import { errorText } from '@/services/settings';

type Mode = 'open' | 'approved';
type RepeaterRef = { id: number; callsign: string };
type Access = { mode: Mode; repeaters: RepeaterRef[] };

const props = defineProps<{ talkgroupId: number }>();

const modeOptions = [
  { value: 'open', label: 'Open' },
  { value: 'approved', label: 'Approved repeaters' },
];

const loaded = ref(false);
const busy = ref(false);
const message = ref('');
const failed = ref(false);
const saved = ref<Access | null>(null);
const draft = reactive<Access>({ mode: 'open', repeaters: [] });
const query = ref('');
const addError = ref('');

const load = (access: Access) => {
  saved.value = access;
  draft.mode = access.mode;
  draft.repeaters = [...access.repeaters];
  addError.value = '';
};

onMounted(async () => {
  try {
    const res = await API.get(`/talkgroups/${props.talkgroupId}/access`);
    load(res.data as Access);
    loaded.value = true;
  } catch (err) {
    console.error(err);
  }
});

const setMode = (mode: string) => {
  draft.mode = mode as Mode;
  addError.value = '';
  message.value = '';
};

const addRepeater = async () => {
  const q = query.value.trim();
  if (!q) return;
  if (!/^\d+$/.test(q)) {
    addError.value = 'No such repeater';
    return;
  }
  try {
    const res = await API.get(`/repeaters/${q}`);
    const rpt = { id: Number(res.data.id), callsign: String(res.data.callsign ?? '') };
    if (!draft.repeaters.some((r) => r.id === rpt.id)) {
      draft.repeaters = [...draft.repeaters, rpt];
    }
    query.value = '';
    addError.value = '';
  } catch {
    addError.value = 'No such repeater';
  }
};

const removeRepeater = (id: number) => {
  draft.repeaters = draft.repeaters.filter((r) => r.id !== id);
};

const reset = () => {
  if (saved.value) load(saved.value);
  message.value = '';
};

const save = async () => {
  message.value = '';
  failed.value = false;
  busy.value = true;
  try {
    const res = await API.patch(`/talkgroups/${props.talkgroupId}/access`, {
      mode: draft.mode,
      repeaters: draft.repeaters.map((r) => r.id),
    });
    load(res.data as Access);
    message.value = 'Saved.';
  } catch (err) {
    failed.value = true;
    message.value = errorText(err, 'Could not save. Try again later.');
  } finally {
    busy.value = false;
  }
};
</script>

<style scoped>
.chip {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  border: 1px solid var(--border);
  border-radius: 9999px;
  padding: 0 0.25rem 0 0.625rem;
  line-height: 1.625rem;
  font-size: 0.8125rem;
  background: var(--muted);
}
.chip-x {
  display: inline-flex;
  width: 1.25rem;
  height: 1.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  color: var(--muted-foreground);
}
.chip-x:hover {
  background: var(--accent);
  color: var(--foreground);
}
</style>
