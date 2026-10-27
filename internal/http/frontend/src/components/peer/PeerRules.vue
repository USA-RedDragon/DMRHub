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
  <Card class="gap-5" data-test="rules">
    <CardHeader><CardTitle>Rules</CardTitle></CardHeader>
    <CardContent>
      <div class="flex flex-col gap-0.5 rounded-[10px] border bg-stripe px-3.5 py-2.5" data-test="rules-summary">
        <span v-for="g in groups" :key="g.key" :class="g.on ? '' : 'text-muted-foreground'" :data-test="`summary-${g.key}`">
          <template v-if="g.on">
            <template v-for="(p, i) in summary(g)" :key="i"><b v-if="p.id" class="font-mono text-base font-semibold">{{ p.text }}</b><template v-else>{{ p.text }}</template></template>
          </template>
          <template v-else>{{ g.off }}</template>
        </span>
      </div>
    </CardContent>
    <CardContent class="flex flex-col gap-5">
      <div v-for="g in groups" :key="g.key" :data-test="`group-${g.key}`">
        <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
          <p class="m-0 text-[15px] font-semibold">{{ g.title }}</p>
          <span class="flex flex-wrap items-center gap-3">
            <label v-if="privateApplies(rulesOf(g))" class="check text-[13px] font-medium">
              <input
                type="checkbox"
                :checked="priv[g.key]"
                :disabled="savingPriv"
                :data-test="`private-${g.key}`"
                @change="setPrivate(g, ($event.target as HTMLInputElement).checked)"
              />Private calls
            </label>
            <ShadButton
              v-if="adding !== g.key"
              type="button"
              variant="outline"
              size="sm"
              class="h-7 px-2.5 text-[13px]"
              :data-test="`add-${g.key}`"
              @click="startAdd(g.key)"
            >Add rule</ShadButton>
          </span>
        </div>
        <div class="overflow-x-auto rounded-[10px] border">
          <table class="w-full border-collapse text-sm">
            <thead>
              <tr>
                <th colspan="2" class="border-b bg-stripe px-3 py-2 text-left font-semibold whitespace-nowrap text-muted-foreground">Calls</th>
                <th class="border-b bg-stripe px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              <tr v-if="!rulesOf(g).length">
                <td colspan="3" class="px-3 py-4 text-center text-muted-foreground">No {{ g.title.toLowerCase() }} rules</td>
              </tr>
              <tr
                v-for="r in rulesOf(g)"
                :key="r.id"
                class="border-b last:border-b-0"
                :class="confirm === r.id ? 'bg-destructive-foreground/8' : ''"
                :data-test="`rule-${r.id}`"
              >
                <td class="w-px px-3 py-2 align-middle whitespace-nowrap text-muted-foreground">{{ ruleLabel(r) }}</td>
                <td v-if="confirm === r.id" colspan="2" class="px-3 py-2 align-middle">
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <span><template v-for="(p, i) in rangeParts(r.subject_id_min, r.subject_id_max)" :key="i"><span v-if="p.id" class="font-mono text-base">{{ p.text }}</span><template v-else>{{ p.text }}</template></template></span>
                    <span class="ml-auto flex flex-wrap items-center gap-2">
                      <span class="text-[13px]">Delete this rule?</span>
                      <ShadButton type="button" variant="outline" size="sm" class="h-7 px-2.5 text-[13px]" @click="confirm = null">Cancel</ShadButton>
                      <ShadButton
                        type="button"
                        variant="outline"
                        size="sm"
                        class="h-7 border-destructive-foreground/50 px-2.5 text-[13px] text-destructive-foreground"
                        :data-test="`really-${r.id}`"
                        @click="remove(r)"
                      >Delete</ShadButton>
                    </span>
                  </div>
                </td>
                <template v-else>
                  <td class="px-3 py-2 align-middle">
                    <template v-for="(p, i) in rangeParts(r.subject_id_min, r.subject_id_max)" :key="i"><span v-if="p.id" class="font-mono text-base">{{ p.text }}</span><template v-else>{{ p.text }}</template></template>
                  </td>
                  <td class="w-px px-3 py-2 text-right align-middle whitespace-nowrap">
                    <ShadButton
                      type="button"
                      variant="outline"
                      size="sm"
                      class="h-7 border-destructive-foreground/50 px-2.5 text-[13px] text-destructive-foreground"
                      :data-test="`delete-${r.id}`"
                      @click="confirm = r.id"
                    >Delete</ShadButton>
                  </td>
                </template>
              </tr>
            </tbody>
          </table>
          <form
            v-if="adding === g.key"
            class="flex flex-wrap items-end gap-2 border-t bg-stripe px-3 py-2.5"
            novalidate
            :data-test="`form-${g.key}`"
            @submit.prevent="submit(g)"
            @keydown.esc="cancelAdd"
          >
            <div>
              <span class="mb-0.5 block text-[13px] font-medium" :id="`kind-${g.key}`">Calls</span>
              <span
                class="inline-flex h-8 items-stretch gap-0.5 rounded-lg border bg-background p-0.5"
                role="group"
                :aria-labelledby="`kind-${g.key}`"
              >
                <button
                  v-for="k in kinds"
                  :key="k.match"
                  type="button"
                  class="rounded-md px-2.5 text-sm"
                  :class="draft.match === k.match ? 'bg-accent font-semibold text-foreground' : 'text-muted-foreground'"
                  :aria-pressed="draft.match === k.match"
                  :data-test="`kind-${k.match}`"
                  @click="draft.match = k.match"
                >{{ k.label }}</button>
              </span>
            </div>
            <div>
              <label class="mb-0.5 block text-[13px] font-medium" :for="`min-${g.key}`">First ID</label>
              <ShadInput
                :id="`min-${g.key}`"
                v-model="draft.min"
                inputmode="numeric"
                autocomplete="off"
                class="h-8 w-[150px] font-mono"
                :aria-invalid="err ? 'true' : undefined"
                data-test="min"
              />
            </div>
            <div>
              <label class="mb-0.5 block text-[13px] font-medium" :for="`max-${g.key}`">Last ID</label>
              <ShadInput
                :id="`max-${g.key}`"
                v-model="draft.max"
                inputmode="numeric"
                autocomplete="off"
                placeholder="Same as first"
                class="h-8 w-[150px] font-mono"
                :aria-invalid="err ? 'true' : undefined"
                data-test="max"
              />
            </div>
            <ShadButton type="button" variant="outline" size="sm" @click="cancelAdd">Cancel</ShadButton>
            <ShadButton type="submit" size="sm" :disabled="busy" data-test="submit">Add</ShadButton>
            <p v-if="err" class="m-0 w-full text-[13px] text-destructive-foreground" data-test="error">{{ err }}</p>
          </form>
        </div>
      </div>
    </CardContent>
  </Card>
</template>

<script lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import {
  MAX_DMR_ID,
  parseID,
  privateApplies,
  rangeParts,
  ruleLabel,
  summaryParts,
  type PeerRule,
  type RuleMatch,
  type TextPart,
} from '@/lib/peerRules';
import API from '@/services/API';
import { errorText } from '@/services/settings';

type GroupKey = 'rx' | 'tx';

type Group = { key: GroupKey; ingress: boolean; title: string; verb: string; on: boolean; off: string };

export default {
  components: { Card, CardContent, CardHeader, CardTitle, ShadButton, ShadInput },
  props: {
    peerId: { type: Number, required: true },
    ingress: { type: Boolean, required: true },
    egress: { type: Boolean, required: true },
    ingressPrivate: { type: Boolean, required: true },
    egressPrivate: { type: Boolean, required: true },
  },
  emits: ['private-changed'],
  data() {
    return {
      rules: [] as PeerRule[],
      priv: { rx: this.ingressPrivate, tx: this.egressPrivate } as Record<GroupKey, boolean>,
      adding: '' as GroupKey | '',
      draft: { match: 'destination' as RuleMatch, min: '', max: '' },
      err: '',
      confirm: null as number | null,
      busy: false,
      savingPriv: false,
      kinds: [
        { match: 'destination' as RuleMatch, label: 'To' },
        { match: 'source' as RuleMatch, label: 'From' },
      ],
    };
  },
  computed: {
    groups(): Group[] {
      return [
        { key: 'rx', ingress: true, title: 'Receive', verb: 'Receives', on: this.ingress, off: 'Receive is off.' },
        { key: 'tx', ingress: false, title: 'Transmit', verb: 'Transmits', on: this.egress, off: 'Transmit is off.' },
      ];
    },
  },
  watch: {
    ingressPrivate(v: boolean) {
      this.priv.rx = v;
    },
    egressPrivate(v: boolean) {
      this.priv.tx = v;
    },
  },
  mounted() {
    API.get(`/peers/${this.peerId}/rules`)
      .then((res) => {
        this.rules = [...(res.data.rules ?? [])];
      })
      .catch((err) => {
        console.error(err);
        toast.error('Error', { description: errorText(err, 'Could not load the rules.') });
      });
  },
  methods: {
    privateApplies,
    rangeParts,
    ruleLabel,
    rulesOf(g: Group): PeerRule[] {
      const own = this.rules.filter((r) => r.direction === g.ingress);
      return [...own.filter((r) => r.match === 'destination'), ...own.filter((r) => r.match === 'source')];
    },
    summary(g: Group): TextPart[] {
      return summaryParts(g.verb, this.rulesOf(g), this.priv[g.key]);
    },
    startAdd(key: GroupKey) {
      this.adding = key;
      this.confirm = null;
      this.err = '';
      this.draft = { match: 'destination', min: '', max: '' };
      this.$nextTick(() => (document.getElementById(`min-${key}`) as HTMLInputElement | null)?.focus());
    },
    cancelAdd() {
      this.adding = '';
      this.err = '';
    },
    submit(g: Group) {
      const min = parseID(this.draft.min);
      const max = this.draft.max.trim() === '' ? min : parseID(this.draft.max);
      if (min === null || max === null) {
        this.err = 'Enter whole numbers.';
        return;
      }
      if (min > max) {
        this.err = 'The last ID must not be lower than the first ID.';
        return;
      }
      if (max === 0) {
        this.err = 'The last ID must be above 0.';
        return;
      }
      if (max > MAX_DMR_ID) {
        this.err = `IDs must be at most ${MAX_DMR_ID}.`;
        return;
      }
      this.busy = true;
      API.post(`/peers/${this.peerId}/rules`, {
        direction: g.ingress,
        match: this.draft.match,
        subject_id_min: min,
        subject_id_max: max,
      })
        .then((res) => {
          this.rules.push(res.data.rule);
          this.adding = '';
          this.err = '';
          toast.success('Rule added', { description: `Peer ${this.peerId}` });
        })
        .catch((err) => {
          console.error(err);
          this.err = errorText(err, 'Could not add the rule.');
        })
        .finally(() => {
          this.busy = false;
        });
    },
    remove(r: PeerRule) {
      API.delete(`/peers/${this.peerId}/rules/${r.id}`)
        .then(() => {
          this.rules = this.rules.filter((x) => x.id !== r.id);
          this.confirm = null;
          toast.success('Rule deleted', { description: `Peer ${this.peerId}` });
        })
        .catch((err) => {
          console.error(err);
          toast.error('Error', { description: errorText(err, 'Could not delete the rule.') });
        });
    },
    setPrivate(g: Group, on: boolean) {
      const field = g.ingress ? 'ingress_private' : 'egress_private';
      const before = this.priv[g.key];
      this.priv[g.key] = on;
      this.savingPriv = true;
      API.patch(`/peers/${this.peerId}`, { [field]: on })
        .then(() => {
          this.$emit('private-changed', { ingress: g.ingress, on });
        })
        .catch((err) => {
          console.error(err);
          this.priv[g.key] = before;
          toast.error('Error', { description: errorText(err, 'Could not save the private call setting.') });
        })
        .finally(() => {
          this.savingPriv = false;
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
</style>
