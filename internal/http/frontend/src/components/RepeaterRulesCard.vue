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
  <Card data-test="repeater-rules">
    <CardHeader>
      <CardTitle>Rules</CardTitle>
    </CardHeader>
    <CardContent v-if="loaded" class="flex flex-col gap-6">
      <section class="flex flex-col gap-3">
        <h3 class="text-sm font-semibold">Slot lock</h3>
        <div class="grid items-start gap-4 md:grid-cols-2">
          <div
            v-for="slot in slots"
            :key="slot"
            class="flex min-w-0 flex-col gap-3 rounded-lg border p-4"
            :data-test="`${slot}-lock`"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <h4 :id="`${slot}-lock-label`" class="text-sm font-semibold">{{ slotTitle(slot) }}</h4>
              <SegmentPicker
                :name="`${slot}-lock`"
                :labelled-by="`${slot}-lock-label`"
                :options="lockOptions"
                :model-value="draft.locked[slot] ? 'lock' : 'open'"
                @update:model-value="(v) => setLock(slot, v === 'lock')"
              />
            </div>
            <template v-if="draft.locked[slot]">
              <div class="flex flex-wrap gap-1.5" :data-test="`${slot}-users`">
                <span v-for="u in draft.users[slot]" :key="u.id" class="chip">
                  <span class="font-mono">{{ u.id }}</span>
                  <span>{{ u.callsign }}</span>
                  <button type="button" class="chip-x" :aria-label="`Remove ${u.id}`" @click="removeUser(slot, u.id)">&times;</button>
                </span>
              </div>
              <form class="flex flex-wrap items-center gap-2" novalidate @submit.prevent="addUser(slot)">
                <ShadInput
                  v-model="userQuery[slot]"
                  class="h-8 min-w-0 flex-[1_1_9rem] font-mono"
                  placeholder="DMR ID or callsign"
                  :aria-label="`Add user to ${slotTitle(slot)}`"
                  :aria-invalid="!!userError[slot]"
                  :data-test="`${slot}-add-user`"
                />
                <ShadButton type="submit" variant="outline" size="sm">Add</ShadButton>
                <p v-if="userError[slot]" class="w-full text-sm text-destructive">{{ userError[slot] }}</p>
              </form>
            </template>
          </div>
        </div>
      </section>
      <Separator />
      <section class="flex flex-col gap-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h3 id="tg-mode-label" class="text-sm font-semibold">Talkgroups</h3>
          <SegmentPicker
            name="tg-mode"
            labelled-by="tg-mode-label"
            :options="modeOptions"
            :model-value="draft.mode"
            @update:model-value="setMode"
          />
        </div>
        <template v-if="draft.mode !== 'off'">
          <div class="flex flex-wrap gap-1.5" data-test="talkgroups">
            <span v-for="tg in draft.talkgroups" :key="tg.id" class="chip">
              <span class="font-mono">{{ tg.id }}</span>
              <span v-if="tg.name">{{ tg.name }}</span>
              <button type="button" class="chip-x" :aria-label="`Remove ${tg.id}`" @click="removeTalkgroup(tg.id)">&times;</button>
            </span>
          </div>
          <form class="flex flex-wrap items-center gap-2" novalidate @submit.prevent="addTalkgroup">
            <ShadInput
              v-model="tgQuery"
              class="h-8 min-w-0 flex-[1_1_9rem] font-mono"
              placeholder="Talkgroup ID"
              aria-label="Add talkgroup"
              :aria-invalid="!!tgError"
              data-test="add-talkgroup"
            />
            <ShadButton type="submit" variant="outline" size="sm">Add</ShadButton>
            <p v-if="tgError" class="w-full text-sm text-destructive">{{ tgError }}</p>
          </form>
        </template>
      </section>
      <div class="flex flex-wrap items-center justify-end gap-2">
        <span v-if="message" class="mr-auto text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'" data-test="rules-message">{{ message }}</span>
        <ShadButton type="button" variant="outline" :disabled="busy" data-test="rules-reset" @click="reset">Reset</ShadButton>
        <ShadButton type="button" :disabled="busy" data-test="rules-save" @click="save">Save</ShadButton>
      </div>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import SegmentPicker from '@/components/SegmentPicker.vue';
import API from '@/services/API';
import { errorText } from '@/services/settings';

type Slot = 'ts1' | 'ts2';
type Mode = 'off' | 'block' | 'allow';
type UserRef = { id: number; callsign: string };
type TalkgroupRef = { id: number; name: string };
type Rules = { ts1_users: UserRef[]; ts2_users: UserRef[]; talkgroup_mode: Mode; talkgroups: TalkgroupRef[] };
type Draft = {
  locked: Record<Slot, boolean>;
  users: Record<Slot, UserRef[]>;
  mode: Mode;
  talkgroups: TalkgroupRef[];
};

const props = defineProps<{ repeaterId: number }>();

const slots: Slot[] = ['ts1', 'ts2'];
const lockOptions = [
  { value: 'open', label: 'Open' },
  { value: 'lock', label: 'Listed users' },
];
const modeOptions = [
  { value: 'off', label: 'Off' },
  { value: 'block', label: 'Block' },
  { value: 'allow', label: 'Allow only' },
];

const loaded = ref(false);
const busy = ref(false);
const message = ref('');
const failed = ref(false);
const saved = ref<Rules | null>(null);
const draft = reactive<Draft>({ locked: { ts1: false, ts2: false }, users: { ts1: [], ts2: [] }, mode: 'off', talkgroups: [] });
const userQuery = reactive<Record<Slot, string>>({ ts1: '', ts2: '' });
const userError = reactive<Record<Slot, string>>({ ts1: '', ts2: '' });
const tgQuery = ref('');
const tgError = ref('');

const slotTitle = (slot: Slot) => (slot === 'ts1' ? 'Timeslot 1' : 'Timeslot 2');

const load = (rules: Rules) => {
  saved.value = rules;
  draft.users = { ts1: [...rules.ts1_users], ts2: [...rules.ts2_users] };
  draft.locked = { ts1: rules.ts1_users.length > 0, ts2: rules.ts2_users.length > 0 };
  draft.mode = rules.talkgroup_mode;
  draft.talkgroups = [...rules.talkgroups];
  userError.ts1 = '';
  userError.ts2 = '';
  tgError.value = '';
};

onMounted(async () => {
  try {
    const res = await API.get(`/repeaters/${props.repeaterId}/rules`);
    load(res.data as Rules);
    loaded.value = true;
  } catch (err) {
    console.error(err);
  }
});

const setLock = (slot: Slot, locked: boolean) => {
  draft.locked[slot] = locked;
  if (!locked) {
    draft.users[slot] = [];
  }
  userError[slot] = '';
  message.value = '';
};

const setMode = (mode: string) => {
  draft.mode = mode as Mode;
  tgError.value = '';
  message.value = '';
};

const addUser = async (slot: Slot) => {
  const q = userQuery[slot].trim();
  if (!q) return;
  try {
    const res = await API.get(`/repeaters/${props.repeaterId}/rules/user?q=${encodeURIComponent(q)}`);
    const user = res.data as UserRef;
    if (!draft.users[slot].some((u) => u.id === user.id)) {
      draft.users[slot] = [...draft.users[slot], user];
    }
    userQuery[slot] = '';
    userError[slot] = '';
  } catch (err) {
    userError[slot] = errorText(err, 'No such user');
  }
};

const removeUser = (slot: Slot, id: number) => {
  draft.users[slot] = draft.users[slot].filter((u) => u.id !== id);
};

const addTalkgroup = async () => {
  const q = tgQuery.value.trim();
  if (!q) return;
  if (!/^\d+$/.test(q)) {
    tgError.value = 'No such talkgroup';
    return;
  }
  try {
    const res = await API.get(`/talkgroups/${q}`);
    const tg = { id: Number(res.data.id), name: String(res.data.name ?? '') };
    if (!draft.talkgroups.some((t) => t.id === tg.id)) {
      draft.talkgroups = [...draft.talkgroups, tg];
    }
    tgQuery.value = '';
    tgError.value = '';
  } catch {
    tgError.value = 'No such talkgroup';
  }
};

const removeTalkgroup = (id: number) => {
  draft.talkgroups = draft.talkgroups.filter((t) => t.id !== id);
};

const reset = () => {
  if (saved.value) load(saved.value);
  message.value = '';
};

const save = async () => {
  message.value = '';
  failed.value = false;
  if (draft.mode !== 'off' && draft.talkgroups.length === 0) {
    failed.value = true;
    message.value = 'List at least one talkgroup';
    return;
  }
  busy.value = true;
  try {
    const res = await API.patch(`/repeaters/${props.repeaterId}/rules`, {
      ts1_users: draft.users.ts1.map((u) => u.id),
      ts2_users: draft.users.ts2.map((u) => u.id),
      talkgroup_mode: draft.mode,
      talkgroups: draft.mode === 'off' ? [] : draft.talkgroups.map((t) => t.id),
    });
    load(res.data as Rules);
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
