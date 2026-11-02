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
    <Card v-if="error && !group">
      <CardHeader>
        <CardTitle>Local group</CardTitle>
      </CardHeader>
      <CardContent>
        <p class="text-destructive" data-test="group-error">{{ error }}</p>
      </CardContent>
    </Card>

    <template v-else-if="loaded">
      <Card data-test="group-card">
        <CardHeader>
          <CardTitle>{{ isNew ? 'New local group' : group?.name }}</CardTitle>
          <CardDescription>
            A call to TG {{ talkgroup || '…' }} on one of these repeaters goes to all of them, and nowhere else.
          </CardDescription>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="grid gap-4 md:grid-cols-2">
            <div class="flex flex-col gap-2">
              <label for="group-name" class="text-sm font-medium">Name</label>
              <ShadInput id="group-name" v-model="name" :disabled="!canManage" maxlength="64" data-test="group-name" />
            </div>
            <div class="flex flex-col gap-2">
              <label for="group-talkgroup" class="text-sm font-medium">Talkgroup</label>
              <ShadSelect
                :model-value="String(talkgroup)"
                :disabled="!canManage"
                @update:model-value="(v) => (talkgroup = Number(v))"
              >
                <SelectTrigger id="group-talkgroup" class="w-full" data-test="group-talkgroup">
                  <SelectValue placeholder="Pick a local talkgroup" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="tg in talkgroupChoices" :key="tg" :value="String(tg)">TG {{ tg }}</SelectItem>
                </SelectContent>
              </ShadSelect>
            </div>
          </div>
          <div v-if="canManage" class="flex flex-wrap items-center gap-3">
            <ShadButton type="button" :disabled="busy" data-test="group-save" @click="save">
              {{ isNew ? 'Make group' : 'Save' }}
            </ShadButton>
            <span v-if="message" class="text-sm" :class="failed ? 'text-destructive' : 'text-muted-foreground'" data-test="group-message">{{ message }}</span>
          </div>
        </CardContent>
      </Card>

      <Card v-if="group" data-test="group-repeaters">
        <CardHeader>
          <CardTitle>Repeaters</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="overflow-x-auto rounded-md border">
            <ShadTable>
              <TableHeader>
                <TableRow>
                  <TableHead>Repeater</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead>Slot</TableHead>
                  <TableHead />
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-if="group.repeaters.length === 0">
                  <TableCell colspan="5" class="text-muted-foreground">No repeaters yet</TableCell>
                </TableRow>
                <TableRow v-for="r in group.repeaters" :key="r.id" :data-test="`member-${r.id}`">
                  <TableCell>{{ r.callsign ? `${r.callsign} — ${r.id}` : r.id }}</TableCell>
                  <TableCell>{{ r.mine ? 'You' : r.owner.callsign }}</TableCell>
                  <TableCell>
                    <ShadSelect
                      :model-value="String(r.slot)"
                      :disabled="!canManage && !r.mine"
                      @update:model-value="(v) => setSlot(r.id, Number(v) as 1 | 2)"
                    >
                      <SelectTrigger class="h-8 w-20" :aria-label="`Slot of ${r.id}`" :data-test="`slot-${r.id}`">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="1">TS1</SelectItem>
                        <SelectItem value="2">TS2</SelectItem>
                      </SelectContent>
                    </ShadSelect>
                  </TableCell>
                  <TableCell>
                    <span
                      v-if="r.state === 'asked'"
                      class="inline-flex items-center rounded-md bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300"
                      data-test="waiting"
                    >Waiting for the owner</span>
                  </TableCell>
                  <TableCell class="text-right">
                    <ShadButton
                      v-if="canManage || r.mine"
                      type="button"
                      variant="outline"
                      size="sm"
                      :disabled="busy"
                      :data-test="`remove-${r.id}`"
                      @click="remove(r.id)"
                    >Remove</ShadButton>
                  </TableCell>
                </TableRow>
              </TableBody>
            </ShadTable>
          </div>
          <form v-if="canManage" class="flex flex-wrap items-center gap-2" novalidate @submit.prevent="add">
            <ShadInput
              v-model="addID"
              class="max-w-[200px]"
              placeholder="Repeater ID"
              inputmode="numeric"
              aria-label="Repeater ID"
              data-test="add-repeater"
            />
            <ShadButton type="submit" variant="outline" :disabled="busy">Add</ShadButton>
            <p v-if="addError" class="w-full text-sm text-destructive" data-test="add-error">{{ addError }}</p>
          </form>
          <p v-if="canManage" class="text-sm text-muted-foreground">
            Your own repeaters join at once. A repeater of another owner joins once its owner says yes on its page.
          </p>
        </CardContent>
      </Card>

      <Card v-if="group && canManage" data-test="group-delete">
        <CardHeader>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>Delete this group</CardTitle>
            <div class="flex items-center gap-2">
              <template v-if="confirming">
                <ShadButton type="button" variant="outline" size="sm" @click="confirming = false">Keep it</ShadButton>
                <ShadButton type="button" variant="destructive" size="sm" :disabled="busy" data-test="group-delete-confirm" @click="destroy">
                  Delete {{ group.name }}
                </ShadButton>
              </template>
              <ShadButton v-else type="button" variant="outline" size="sm" class="text-destructive" data-test="group-delete-ask" @click="confirming = true">
                Delete
              </ShadButton>
            </div>
          </div>
        </CardHeader>
      </Card>
    </template>
  </div>
</template>

<script lang="ts">
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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
  addLocalGroupRepeater,
  createLocalGroup,
  deleteLocalGroup,
  getLocalGroup,
  listLocalGroups,
  removeLocalGroupRepeater,
  saveLocalGroup,
  setLocalGroupSlot,
  type LocalGroup,
} from '@/services/localGroups';
import { errorText } from '@/services/settings';

export default {
  components: {
    ShadButton,
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    ShadInput,
    ShadSelect,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    ShadTable: Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  },
  head: {
    title: 'Local group',
  },
  data: function() {
    return {
      group: null as LocalGroup | null,
      talkgroups: [] as number[],
      name: '',
      talkgroup: 0,
      loaded: false,
      error: '',
      message: '',
      failed: false,
      busy: false,
      addID: '',
      addError: '',
      confirming: false,
    };
  },
  computed: {
    isNew(): boolean {
      return this.$route.params.id === 'new';
    },
    canManage(): boolean {
      return this.isNew || !!this.group?.can_manage;
    },
    talkgroupChoices(): number[] {
      const all = new Set(this.talkgroups);
      if (this.group) all.add(this.group.talkgroup);
      return [...all].sort((a, b) => a - b);
    },
  },
  watch: {
    '$route.params.id'() {
      this.load();
    },
  },
  mounted() {
    this.load();
  },
  methods: {
    async load() {
      this.loaded = false;
      this.error = '';
      this.message = '';
      try {
        const list = await listLocalGroups();
        this.talkgroups = list.talkgroups ?? [];
        if (this.isNew) {
          this.group = null;
          this.name = '';
          this.talkgroup = this.talkgroups[0] ?? 0;
        } else {
          this.show(await getLocalGroup(Number(this.$route.params.id)));
        }
        this.loaded = true;
      } catch (err) {
        this.error = errorText(err, 'Could not load the local group');
      }
    },
    show(g: LocalGroup) {
      this.group = g;
      this.name = g.name;
      this.talkgroup = g.talkgroup;
    },
    async run(fn: () => Promise<void>, fallback: string, target: 'message' | 'addError' = 'message') {
      this.busy = true;
      this.failed = false;
      this[target] = '';
      try {
        await fn();
      } catch (err) {
        this.failed = true;
        this[target] = errorText(err, fallback);
      } finally {
        this.busy = false;
      }
    },
    save() {
      return this.run(async () => {
        if (this.isNew) {
          const g = await createLocalGroup(this.name, this.talkgroup);
          this.$router.push(`/localgroups/${g.id}`);
          return;
        }
        this.show(await saveLocalGroup(this.group!.id, this.name, this.talkgroup));
        this.message = 'Saved';
      }, 'Could not save the group');
    },
    add() {
      const id = Number(this.addID.trim());
      if (!/^\d{6,9}$/.test(this.addID.trim())) {
        this.addError = 'Type the 6 to 9 digit ID of a repeater or hotspot';
        return;
      }
      return this.run(async () => {
        this.show(await addLocalGroupRepeater(this.group!.id, id, 2));
        this.addID = '';
      }, 'Could not add the repeater', 'addError');
    },
    setSlot(repeater: number, slot: 1 | 2) {
      return this.run(async () => {
        this.show(await setLocalGroupSlot(this.group!.id, repeater, slot));
      }, 'Could not change the slot');
    },
    remove(repeater: number) {
      return this.run(async () => {
        await removeLocalGroupRepeater(this.group!.id, repeater);
        if (!this.canManage) {
          this.$router.push('/repeaters?view=local');
          return;
        }
        this.show(await getLocalGroup(this.group!.id));
      }, 'Could not remove the repeater');
    },
    destroy() {
      return this.run(async () => {
        await deleteLocalGroup(this.group!.id);
        this.$router.push('/repeaters?view=local');
      }, 'Could not delete the group');
    },
  },
};
</script>
