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
  <div class="mx-auto max-w-[1180px]">
    <h1 class="mb-1 text-2xl font-bold">Setup</h1>
    <p class="mb-4.5 text-muted-foreground">
      Settings that you save here go into effect at once on every replica, without a restart.
    </p>
    <div
      v-if="applyErrors.length > 0"
      role="alert"
      class="my-4 flex flex-col gap-2 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm"
      data-test="apply-errors"
    >
      <b class="font-semibold">{{ applyHeading }}</b>
      <p v-for="(e, i) in applyErrors" :key="i">
        {{ e.summary }}<br />
        <span class="font-mono text-xs break-all text-muted-foreground">{{ e.detail }}</span>
      </p>
      <p v-if="applyOK">{{ applyOK }}</p>
    </div>
    <div
      v-if="setupState.disagree"
      role="status"
      class="my-4 flex flex-col gap-2 rounded-lg border border-amber-500/55 bg-amber-500/10 px-4 py-3 text-sm"
      data-test="replicas-differ"
    >
      <b class="font-semibold">The replicas do not run with the same settings.</b>
      <p>
        {{ differText }} A setting that the deployment sets on one replica stays as the deployment sets it there. Look
        for the amber mark on the tabs.
      </p>
    </div>
    <div
      v-if="unanswered.length > 0"
      role="status"
      class="my-4 rounded-lg border px-4 py-3 text-sm text-muted-foreground"
      data-test="replicas-unanswered"
    >
      {{ unansweredText }}
    </div>
    <div
      class="mb-5 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-[10px] border bg-stripe px-3.5 py-2 text-sm text-muted-foreground"
      data-test="setup-status"
    >
      <span v-if="setupState.replicas.length > 0" class="inline-flex items-center gap-1.5 rounded-full border px-2.5 text-xs leading-[22px]">
        <span class="size-[7px] rounded-full" :class="allCurrent ? 'bg-green-600 dark:bg-green-400' : 'bg-amber-500'" />
        {{ replicaCount }}
      </span>
      <span v-if="setupState.loaded">{{ revisionText }}</span>
      <span class="inline-flex items-center gap-1.5 rounded-full border px-2 text-xs font-semibold leading-5 text-foreground">
        <span class="size-1.5 rounded-full bg-green-600 dark:bg-green-400" />Saved in Setup
      </span>
      <span class="inline-flex items-center gap-1.5 rounded-full border px-2 text-xs font-semibold leading-5">
        <span class="size-1.5 rounded-full bg-muted-foreground" />Default
      </span>
      <span class="inline-flex items-center gap-1.5 rounded-full border px-2 text-xs font-semibold leading-5">
        <Lock class="size-3" aria-hidden="true" />Set by the deployment
      </span>
      <span class="inline-flex items-center gap-1.5 rounded-full border px-2 text-xs font-semibold leading-5">
        <span class="size-1.5 rounded-[2px] border border-muted-foreground" />Read at start
      </span>
    </div>
    <div class="grid items-start gap-7 md:grid-cols-[240px_minmax(0,1fr)]">
      <nav
        class="flex gap-1 overflow-x-auto rounded-xl border bg-sidebar p-1.5 md:sticky md:top-4 md:block md:max-h-[calc(100vh-2rem)] md:overflow-y-auto md:px-2 md:py-2.5"
        aria-label="Setup areas"
        data-test="setup-menu"
      >
        <template v-for="g in groups" :key="g.name">
          <h4 class="mx-2.5 mt-2.5 mb-1 hidden text-[11px] font-bold tracking-[0.06em] text-muted-foreground uppercase first:mt-0.5 md:block">
            {{ g.name }}
          </h4>
          <RouterLink
            v-for="t in g.tabs"
            :key="t.id"
            :to="{ hash: `#${t.id}` }"
            class="no-underline flex items-center gap-2 rounded-[7px] px-2.5 py-1.5 font-medium whitespace-nowrap text-foreground hover:bg-sidebar-accent"
            :class="t.id === tab.id ? 'bg-sidebar-accent font-bold shadow-[inset_3px_0_0_var(--primary)]' : ''"
            :aria-current="t.id === tab.id ? 'page' : undefined"
            :data-test="`menu-${t.id}`"
          >
            {{ t.name }}
            <span
              v-if="dirty[t.id]"
              class="ml-auto size-2 shrink-0 rounded-full bg-amber-500"
              title="Unsaved changes"
              :data-test="`menu-dirty-${t.id}`"
            ><span class="sr-only">Unsaved changes</span></span>
            <span
              v-else-if="startOnly(t)"
              class="ml-auto hidden text-[11px] font-medium text-muted-foreground md:inline"
            >Read at start</span>
          </RouterLink>
        </template>
      </nav>
      <section :data-test="`tab-${tab.id}`" :aria-labelledby="`tab-title-${tab.id}`">
        <div class="mb-3.5 flex flex-wrap items-end justify-between gap-2">
          <h2 :id="`tab-title-${tab.id}`" class="text-[22px] leading-tight font-bold">{{ tab.name }}</h2>
          <span class="text-xs text-muted-foreground">Link: <code class="font-mono">/admin/setup#{{ tab.id }}</code></span>
        </div>
        <p v-if="tab.intro.length > 0" class="mb-4 max-w-[70ch] text-muted-foreground">
          <template v-for="(part, i) in tab.intro" :key="i">
            <template v-if="typeof part === 'string'">{{ part }}</template>
            <RouterLink v-else :to="part.to">{{ part.text }}</RouterLink>
          </template>
        </p>
        <div
          v-if="startOnly(tab)"
          class="mb-4 flex items-start gap-2.5 rounded-[10px] border bg-muted px-3.5 py-2.5 text-sm"
          data-test="section-note"
        >
          <span class="inline-flex items-center gap-1.5 rounded-full border px-2 text-xs font-semibold leading-5 whitespace-nowrap text-muted-foreground">
            <span class="size-1.5 rounded-[2px] border border-muted-foreground" />Read at start
          </span>
          <span>DMRHub reads these at start. Change them in the config file or the environment, then restart DMRHub.</span>
        </div>
        <template v-if="setupState.loaded">
          <TranscodingTab
            v-if="tab.custom === 'transcoding'"
            :value="read(ambeServers)"
            :setting="setupState.settings[ambeServers]"
            :error="errors[ambeServers]"
            :saves="saves"
            @set="set"
          />
          <SetupCard
            v-for="c in tab.cards"
            v-else
            :key="c.title"
            :card="c"
            :read="read"
            :settings="setupState.settings"
            :errors="errors"
            :secrets="secrets"
            :only="tab.optional ? Object.keys(setupState.settings) : undefined"
            @set="set"
          />
        </template>
        <div
          class="sticky bottom-0 z-[3] mt-1 flex flex-wrap items-center justify-end gap-2 border-t bg-background py-3"
          data-test="savebar"
        >
          <span
            class="mr-auto text-sm"
            :class="savedMsg[tab.id] ? 'text-green-700 dark:text-green-400' : 'text-muted-foreground'"
            data-test="savebar-message"
          >{{ saveText }}</span>
          <template v-if="!readOnly">
            <ShadButton type="button" variant="outline" size="sm" :disabled="!dirty[tab.id] || saving" data-test="reset" @click="reset">
              Reset
            </ShadButton>
            <ShadButton type="button" size="sm" :disabled="!dirty[tab.id] || saving" data-test="save" @click="save">
              Save {{ tab.name }}
            </ShadButton>
          </template>
        </div>
      </section>
    </div>
  </div>
</template>

<script lang="ts">
import type { AxiosResponse } from 'axios';
import { defineComponent } from 'vue';
import { RouterLink } from 'vue-router';
import { Lock } from 'lucide-vue-next';

import SetupCard from '@/components/setup/menu/SetupCard.vue';
import TranscodingTab from '@/components/setup/menu/TranscodingTab.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { ambeServersError, type AmbeServerRow } from '@/lib/codec';
import {
  AMBE_SERVERS, clone, getAt, keepEdits, setAt, tabCheckErrors, tabDirty, tabFields, tabOfHash, tabPaths, tabPayload,
  tabReadOnly, tabStartOnly, visibleTabs, TABS, type FieldDef, type TabDef, type TabGroup, type Values,
} from '@/lib/setupMenu';
import {
  emptySetupState, replicaName, serverName, settingLabel, setupStateKey, setupStateOf, stateKeys, type SetupState,
} from '@/lib/setupState';
import API from '@/services/API';

type ApplyError = { replica?: string; server: string; field?: string; running: boolean; error: string };
type SaveReplica = { id: string; self: boolean; answered: boolean };
type ShownApplyError = { summary: string; detail: string };

const asRecord = (v: unknown): Record<string, unknown> | undefined =>
  typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined;

const listNames = (names: string[]): string => {
  if (names.length <= 1) {
    return names.join('');
  }
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
};

const flatErrors = (v: unknown, prefix = '', out: Record<string, string> = {}): Record<string, string> => {
  const r = asRecord(v);
  if (!r) {
    return out;
  }
  for (const [k, e] of Object.entries(r)) {
    const path = prefix ? `${prefix}.${k}` : k;
    if (typeof e === 'string') {
      out[path] = e;
    } else {
      flatErrors(e, path, out);
    }
  }
  return out;
};

const showApplyError = (e: ApplyError, self: boolean): ShownApplyError => {
  const replica = e.replica ? replicaName(e.replica) : '';
  if (e.field) {
    return {
      summary: `Replica ${replica} cannot run with the saved ${settingLabel(e.field)}. It keeps its previous settings.`,
      detail: e.error,
    };
  }
  const state = e.running ? 'It keeps running with its previous settings.' : 'It is not running.';
  const of = self || !replica ? '' : ` of replica ${replica}`;
  return { summary: `The ${serverName(e.server)} server${of} could not start with the new settings. ${state}`, detail: e.error };
};

const fieldsByPath: Record<string, FieldDef> = Object.fromEntries(TABS.flatMap(tabFields).map((f) => [f.path, f]));

const groupNames: TabGroup[] = ['Server', 'DMR', 'Features'];

const now = () => new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', timeZoneName: 'short' });

export default defineComponent({
  components: { Lock, RouterLink, SetupCard, ShadButton, TranscodingTab },
  head() {
    return { title: 'Admin Setup' };
  },
  provide() {
    return { [setupStateKey as symbol]: this.setupState };
  },
  data() {
    return {
      original: {} as Values,
      edited: {} as Values,
      setupState: emptySetupState() as SetupState,
      secrets: {} as Record<string, boolean>,
      errors: {} as Record<string, string>,
      savedMsg: {} as Record<string, string>,
      failMsg: '',
      saving: false,
      saves: 0,
      applyErrors: [] as ShownApplyError[],
      failedReplicas: [] as string[],
      applyOK: '',
      unanswered: [] as string[],
      ambeServers: AMBE_SERVERS,
    };
  },
  computed: {
    tabs(): TabDef[] {
      return visibleTabs(this.setupState);
    },
    tab(): TabDef {
      return tabOfHash(this.$route.hash, this.tabs);
    },
    groups(): { name: TabGroup; tabs: TabDef[] }[] {
      return groupNames.map((name) => ({ name, tabs: this.tabs.filter((t) => t.group === name) })).filter((g) => g.tabs.length > 0);
    },
    dirty(): Record<string, boolean> {
      return Object.fromEntries(this.tabs.map((t) => [t.id, tabDirty(t, this.original, this.edited)]));
    },
    readOnly(): boolean {
      return tabReadOnly(this.tab, this.setupState);
    },
    saveText(): string {
      if (this.readOnly) {
        return 'Nothing on this tab can change while DMRHub runs.';
      }
      if (this.savedMsg[this.tab.id]) {
        return this.savedMsg[this.tab.id] as string;
      }
      if (this.failMsg) {
        return this.failMsg;
      }
      const extra = this.tab.saveNote ?? 'Saves only the settings on this tab.';
      return this.dirty[this.tab.id] ? `You have unsaved changes on this tab. ${extra}` : extra;
    },
    allCurrent(): boolean {
      return this.setupState.replicas.every((r) => r.current);
    },
    replicaCount(): string {
      const n = this.setupState.replicas.length;
      return n === 1 ? '1 replica' : `${n} replicas`;
    },
    revisionText(): string {
      const rev = this.setupState.revision;
      if (rev === 0) {
        return 'Nothing is saved in Setup yet.';
      }
      const n = this.setupState.replicas.length;
      if (!this.allCurrent) {
        const behind = this.setupState.replicas.filter((r) => !r.current).map((r) => replicaName(r.id));
        return `The latest save is revision ${rev}. Replica ${listNames(behind)} does not run it yet.`;
      }
      if (n <= 1) {
        return `It runs revision ${rev}.`;
      }
      return n === 2 ? `Both run revision ${rev}.` : `All run revision ${rev}.`;
    },
    applyHeading(): string {
      if (this.failedReplicas.length === 0) {
        return 'The settings were saved, but some could not be put into effect.';
      }
      const names = this.failedReplicas.map(replicaName);
      const who = names.length === 1 ? `replica ${names[0]}` : `replicas ${listNames(names)}`;
      return `The settings were saved, but ${who} could not put all of them into effect.`;
    },
    differText(): string {
      const paths = Object.entries(this.setupState.settings).filter(([, s]) => s.disagree).map(([p]) => settingLabel(p));
      const n = paths.length;
      return `${n} ${n === 1 ? 'setting differs' : 'settings differ'} between replicas: ${listNames(paths)}.`;
    },
    unansweredText(): string {
      return `Replica ${listNames(this.unanswered.map(replicaName))} did not report in time.`;
    },
  },
  watch: {
    'tab.id'() {
      this.failMsg = '';
      window.scrollTo(0, 0);
    },
  },
  created() {
    this.load(null);
  },
  methods: {
    read(path: string): unknown {
      return getAt(this.edited, path);
    },
    startOnly(t: TabDef): boolean {
      return tabStartOnly(t, this.setupState) && tabReadOnly(t, this.setupState);
    },
    set(path: string, value: unknown) {
      setAt(this.edited, path, clone(value));
      for (const [p, v] of fieldsByPath[path]?.also?.(value, this.read) ?? []) {
        setAt(this.edited, p, clone(v));
      }
      delete this.savedMsg[this.tab.id];
      delete this.errors[path];
      this.failMsg = '';
    },
    reset() {
      for (const p of tabPaths(this.tab)) {
        setAt(this.edited, p, clone(getAt(this.original, p)));
        delete this.errors[p];
      }
      this.failMsg = '';
    },
    async load(saved: TabDef | null) {
      try {
        const response: AxiosResponse = await API.get('/config');
        const data = asRecord(response.data) ?? {};
        const secrets = asRecord(data.secrets) ?? {};
        this.secrets = {
          'secret': secrets.secretSet === true,
          'password-salt': secrets.passwordSaltSet === true,
          'smtp.password': secrets.smtpPasswordSet === true,
        };
        Object.assign(this.setupState, setupStateOf(data));
        const fresh = clone(data);
        for (const key of stateKeys) {
          delete fresh[key];
        }
        const edited = this.setupState.loaded && Object.keys(this.original).length > 0
          ? keepEdits(fresh, this.original, this.edited, saved)
          : clone(fresh);
        this.original = fresh;
        this.edited = edited;
      } catch {
        this.$toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to load the settings', life: 3000 });
      }
    },
    localError(tab: TabDef): Record<string, string> {
      if (tab.custom !== 'transcoding') {
        return tabCheckErrors(tab, this.read);
      }
      const rows = getAt(this.edited, AMBE_SERVERS);
      const error = ambeServersError(Array.isArray(rows) ? (rows as AmbeServerRow[]) : []);
      return error ? { [AMBE_SERVERS]: error } : {};
    },
    async save() {
      const tab = this.tab;
      const local = this.localError(tab);
      if (Object.keys(local).length > 0) {
        this.errors = local;
        this.failMsg = 'Some settings on this tab are not valid. Fix the marked fields and save again.';
        return;
      }
      const payload = tabPayload(tab, this.original, this.edited);
      this.saving = true;
      try {
        const check = await API.post('/config/validate', payload);
        if (!check.data?.valid) {
          this.errors = flatErrors(check.data?.errors);
          const here = tabPaths(tab).some((p) => p in this.errors);
          this.failMsg = here
            ? 'Some settings on this tab are not valid. Fix the marked fields and save again.'
            : `Some saved settings on other tabs are not valid: ${listNames(Object.keys(this.errors).map(settingLabel))}.`;
          return;
        }
        this.errors = {};
        const response = await API.put('/config', payload);
        this.saves++;
        this.applyErrors = [];
        this.failedReplicas = [];
        this.applyOK = '';
        const replicas = (asRecord(response?.data)?.replicas ?? []) as SaveReplica[];
        this.unanswered = replicas.filter((r) => !r.self && !r.answered).map((r) => r.id);
        const others = replicas.length > 1 ? ' Every replica that answered put it into effect.' : '';
        await this.load(tab);
        this.savedMsg[tab.id] = `Saved at ${now()}.${others}`;
      } catch (error: unknown) {
        const res = asRecord(asRecord(error)?.response);
        const data = asRecord(res?.data);
        if (res?.status === 409 && Array.isArray(data?.applyErrors)) {
          const failures = data.applyErrors as ApplyError[];
          const replicas = Array.isArray(data.replicas) ? (data.replicas as SaveReplica[]) : [];
          const self = replicas.find((r) => r.self)?.id;
          this.saves++;
          this.applyErrors = failures.map((e) => showApplyError(e, !e.replica || e.replica === self));
          this.failedReplicas = [...new Set(failures.map((e) => e.replica ?? ''))].filter((r) => r !== '' && r !== self);
          const selfFailed = failures.some((e) => !e.replica || e.replica === self);
          this.applyOK = self && !selfFailed && this.failedReplicas.length > 0
            ? `Replica ${replicaName(self)} runs with the new settings.`
            : '';
          this.unanswered = replicas.filter((r) => !r.self && !r.answered).map((r) => r.id);
          await this.load(tab);
          this.savedMsg[tab.id] = `Saved at ${now()}, but some settings could not be put into effect.`;
          return;
        }
        if (res?.status === 400 && data?.errors) {
          this.errors = flatErrors(data.errors);
        }
        this.failMsg = typeof data?.error === 'string' ? `The settings were not saved: ${data.error}.` : 'The settings were not saved.';
        this.$toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to save the settings', life: 3000 });
      } finally {
        this.saving = false;
      }
    },
  },
});
</script>
