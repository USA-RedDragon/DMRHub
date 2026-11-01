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
    <h1 class="mb-1 text-2xl font-bold">Set up DMRHub</h1>
    <div
      v-if="closed"
      role="alert"
      class="my-4 flex flex-col gap-2 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm"
      data-test="wizard-closed"
    >
      <b class="font-semibold">{{ closedText.heading }}</b>
      <p>{{ closedText.advice }}</p>
    </div>
    <template v-else>
      <p class="mb-5 text-muted-foreground">Check each area, then save. You can change all of it later in Admin, Setup.</p>
      <div
        v-if="fixLost"
        role="alert"
        class="my-4 flex flex-col gap-2 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm"
        data-test="fix-lost"
      >
        <b class="font-semibold">DMRHub did not answer after the database fix.</b>
        <p>Check that DMRHub runs. This page goes on to DMRHub once it answers.</p>
      </div>
      <div
        v-if="databasePhase"
        role="alert"
        class="my-4 flex flex-col gap-2 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm"
        data-test="database-problem"
      >
        <b class="font-semibold">DMRHub cannot reach its database.</b>
        <p>{{ databaseAdvice }}</p>
        <p v-if="setupState.wizard?.database_error" class="font-mono text-xs break-all text-muted-foreground">
          {{ setupState.wizard.database_error }}
        </p>
      </div>
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
      <SetupMenu
        :tabs="tabs"
        :tab="tab"
        :state="setupState"
        :read="read"
        :errors="shownErrors"
        :secrets="secrets"
        :saves="saves"
        :marks="marks"
        wizard
        @set="set"
      >
        <template #tab>
          <div v-if="tab.id === 'database' && databaseEditable" class="-mt-2 mb-5 flex flex-wrap items-center gap-3" data-test="database-test">
            <ShadButton type="button" variant="outline" size="sm" :disabled="testing || saving" @click="testDatabase">
              Test the connection
            </ShadButton>
            <span
              v-if="tested.text"
              class="text-sm break-all"
              :class="tested.reached ? 'text-green-700 dark:text-green-400' : 'text-destructive-foreground'"
              data-test="database-test-result"
            >{{ tested.text }}</span>
          </div>
          <p
            v-else-if="tab.id === 'database' && setupState.loaded && !databasePhase"
            class="-mt-2 mb-5 text-sm text-green-700 dark:text-green-400"
            data-test="database-reached"
          >Connected.</p>
        </template>
        <template #savebar>
          <div
            class="sticky bottom-0 z-[3] mt-1 flex flex-wrap items-center justify-end gap-2 border-t bg-background py-3"
            data-test="savebar"
          >
            <span
              class="mr-auto text-sm"
              :class="saveText.error ? 'text-destructive-foreground' : 'text-muted-foreground'"
              data-test="savebar-message"
            >{{ saveText.text }}</span>
            <ShadButton type="button" size="sm" :disabled="saving || !setupState.loaded" data-test="save" @click="save">
              Save and continue
            </ShadButton>
          </div>
        </template>
      </SetupMenu>
    </template>
  </div>
</template>

<script lang="ts">
import type { AxiosResponse } from 'axios';
import { defineComponent } from 'vue';

import SetupMenu from '@/components/setup/menu/SetupMenu.vue';
import { Button as ShadButton } from '@/components/ui/button';
import { ambeServersError, type AmbeServerRow } from '@/lib/codec';
import {
  AMBE_SERVERS, allPayload, clone, getAt, setAt, tabCheckErrors, tabFields, tabOfHash, tabOfPath, tabReadOnly, wizardTabs, TABS,
  type FieldDef, type TabDef, type TabMark, type Values,
} from '@/lib/setupMenu';
import {
  emptySetupState, replicaName, serverName, settingLabel, setupStateKey, setupStateOf, stateKeys, type SetupState,
} from '@/lib/setupState';
import API from '@/services/API';

type ApplyError = { replica?: string; server: string; field?: string; running: boolean; error: string };
type SaveReplica = { id: string; self: boolean; answered: boolean };
type ShownApplyError = { summary: string; detail: string };
type ClosedReason = '' | 'database' | 'settings';
type FixAnswer = { status: 'pending' | 'done' | 'failed'; phase?: unknown; error?: unknown; errors?: unknown };

const fixPollInterval = 1000;
const fixConfirmTries = 60;
const fixLostInterval = 5000;

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

const fixAnswerOf = (data: unknown): FixAnswer | undefined => {
  const d = asRecord(data);
  const status = d?.status;
  if (status !== 'pending' && status !== 'done' && status !== 'failed') {
    return undefined;
  }
  return { status, phase: d?.phase, error: d?.error, errors: d?.errors };
};

const closedReasonOf = (data: unknown): ClosedReason => {
  const closed = asRecord(data)?.closed;
  return closed === 'database' || closed === 'settings' ? closed : '';
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

export default defineComponent({
  components: { SetupMenu, ShadButton },
  head() {
    return { title: 'Setup' };
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
      failMsg: '',
      saving: false,
      saves: 0,
      testing: false,
      tested: { reached: false, text: '' },
      applyErrors: [] as ShownApplyError[],
      failedReplicas: [] as string[],
      applyOK: '',
      unanswered: [] as string[],
      closed: '' as ClosedReason,
      fixPoll: undefined as ReturnType<typeof setTimeout> | undefined,
      fixLost: false,
    };
  },
  computed: {
    tabs(): TabDef[] {
      return wizardTabs(this.setupState);
    },
    tab(): TabDef {
      return tabOfHash(this.$route.hash, this.tabs);
    },
    openTabs(): TabDef[] {
      return this.tabs.filter((t) => !tabReadOnly(t, this.setupState));
    },
    databasePhase(): boolean {
      return this.setupState.wizard?.phase === 'database';
    },
    databaseEditable(): boolean {
      return this.databasePhase && this.openTabs.some((t) => t.id === 'database');
    },
    checked(): Record<string, string> {
      const out: Record<string, string> = {};
      for (const t of this.openTabs) {
        Object.assign(out, this.localError(t));
      }
      return out;
    },
    shownErrors(): Record<string, string> {
      return { ...this.errors, ...this.checked };
    },
    marks(): Record<string, TabMark> {
      const out: Record<string, TabMark> = {};
      for (const p of Object.keys(this.shownErrors)) {
        const t = tabOfPath(p);
        if (t) {
          out[t.id] = 'error';
        }
      }
      return out;
    },
    saveText(): { text: string; error: boolean } {
      const paths = Object.keys(this.shownErrors);
      if (paths.length === 0) {
        return { text: this.failMsg, error: this.failMsg !== '' };
      }
      const onTabs = paths.filter((p) => tabOfPath(p));
      if (onTabs.length === 0) {
        return { text: `Some settings are not valid: ${listNames(paths.map(settingLabel))}.`, error: true };
      }
      const names = [...new Set(onTabs.map((p) => tabOfPath(p)!.name))];
      const only = onTabs[0] as string;
      if (onTabs.length === 1 && this.shownErrors[only] === 'Required.') {
        return { text: `Set the ${fieldsByPath[only]?.label ?? settingLabel(only)} on the ${names[0]} tab.`, error: true };
      }
      const which = names.length === 1 ? `the ${names[0]} tab` : `the ${listNames(names)} tabs`;
      return { text: `Fix the marked settings on ${which}.`, error: true };
    },
    databaseAdvice(): string {
      const w = this.setupState.wizard;
      if (w?.database_locked) {
        return 'The deployment sets the database. Fix it in the config file or the environment, then restart DMRHub.';
      }
      if (w?.store_error) {
        return `The setup wizard cannot keep a database setting here: ${w.store_error}. Set the database in the config file or the environment of the deployment, then restart DMRHub.`;
      }
      return `Fix the settings on the Database tab and save. DMRHub tries them at once and keeps them in ${w?.database_file ?? 'its working directory'}. The other tabs open once DMRHub reaches its database.`;
    },
    closedText(): { heading: string; advice: string } {
      if (this.closed === 'database') {
        return {
          heading: 'DMRHub cannot reach its database.',
          advice: 'A database setting was saved before, so the setup wizard cannot change it. An admin must fix the database in the config file, the environment or the flags, then restart DMRHub.',
        };
      }
      return {
        heading: 'DMRHub needs an admin to fix its settings.',
        advice: 'DMRHub has an admin, so the setup wizard cannot change the settings. An admin must fix them in the config file, the environment or the flags, then restart DMRHub.',
      };
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
      window.scrollTo(0, 0);
    },
  },
  created() {
    API.get('/setupwizard')
      .then((res: AxiosResponse) => {
        const closed = closedReasonOf(res.data);
        if (closed) {
          this.closed = closed;
          return;
        }
        if (res.data?.config_valid && res.data?.first_admin_open) {
          this.$router.replace('/setup/user');
        }
        this.load();
      })
      .catch(() => {
        this.load();
      });
  },
  unmounted() {
    clearTimeout(this.fixPoll);
  },
  methods: {
    read(path: string): unknown {
      return getAt(this.edited, path);
    },
    set(path: string, value: unknown) {
      setAt(this.edited, path, clone(value));
      for (const [p, v] of fieldsByPath[path]?.also?.(value, this.read) ?? []) {
        setAt(this.edited, p, clone(v));
      }
      delete this.errors[path];
      this.failMsg = '';
      if (path.startsWith('database.')) {
        this.tested = { reached: false, text: '' };
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
    async load() {
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
        this.original = fresh;
        this.edited = clone(fresh);
        this.errors = {};
      } catch (error: unknown) {
        const closed = closedReasonOf(asRecord(asRecord(error)?.response)?.data);
        if (closed) {
          this.closed = closed;
          return;
        }
        this.$toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to load the settings', life: 3000 });
        return;
      }
      if (!this.databasePhase) {
        await this.validate(this.original);
      }
    },
    async validate(payload: Values): Promise<boolean> {
      try {
        const check = await API.post('/config/validate', payload);
        if (check.data?.valid) {
          return true;
        }
        this.errors = flatErrors(check.data?.errors);
        return false;
      } catch {
        return true;
      }
    },
    showFirstError() {
      const paths = Object.keys(this.shownErrors);
      if (paths.some((p) => tabOfPath(p)?.id === this.tab.id)) {
        return;
      }
      const first = this.tabs.find((t) => paths.some((p) => tabOfPath(p)?.id === t.id));
      if (first) {
        this.$router.replace({ hash: `#${first.id}` });
      }
    },
    async testDatabase() {
      this.testing = true;
      this.tested = { reached: false, text: '' };
      try {
        const res = await API.post('/setupwizard/database-test', allPayload(this.original, this.edited));
        const data = asRecord(res.data) ?? {};
        if (data.reached === true) {
          this.tested = { reached: true, text: 'Connected.' };
        } else {
          this.tested = { reached: false, text: typeof data.error === 'string' ? data.error : 'DMRHub cannot reach this database.' };
        }
      } catch (error: unknown) {
        const data = asRecord(asRecord(asRecord(error)?.response)?.data);
        if (data?.errors) {
          this.errors = flatErrors(data.errors);
        }
        this.tested = { reached: false, text: typeof data?.error === 'string' ? data.error : 'The test did not run.' };
      } finally {
        this.testing = false;
      }
    },
    async save() {
      if (Object.keys(this.shownErrors).length > 0) {
        this.showFirstError();
        return;
      }
      const payload = allPayload(this.original, this.edited);
      this.saving = true;
      try {
        if (!await this.validate(payload)) {
          this.showFirstError();
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
        if (this.databasePhase) {
          if (fixAnswerOf(response?.data)?.status === 'pending') {
            this.pollDatabaseFix();
            return;
          }
          this.afterDatabase(asRecord(response?.data)?.phase);
          return;
        }
        this.$toast.add({ severity: 'success', summary: 'Success', detail: 'The settings were saved', life: 3000 });
        this.$router.push('/setup/user');
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
          await this.load();
          this.failMsg = 'The settings were saved, but some could not be put into effect.';
          return;
        }
        this.saveFailed(data);
      } finally {
        this.saving = false;
      }
    },
    saveFailed(data: Record<string, unknown> | undefined) {
      if (data?.errors) {
        this.errors = flatErrors(data.errors);
        this.showFirstError();
      }
      this.failMsg = typeof data?.error === 'string' ? `The settings were not saved: ${data.error}` : 'The settings were not saved.';
      this.$toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to save the settings', life: 3000 });
    },
    pollDatabaseFix(misses = 0) {
      API.get('/setupwizard/database-fix')
        .then((res: AxiosResponse) => {
          const answer = fixAnswerOf(res.data);
          if (!answer) {
            this.confirmStart(misses);
          } else if (answer.status === 'pending') {
            this.fixPoll = setTimeout(() => this.pollDatabaseFix(), fixPollInterval);
          } else if (answer.status === 'failed') {
            this.saveFailed(asRecord(res.data));
          } else {
            this.afterDatabase(answer.phase);
          }
        })
        .catch(() => {
          this.confirmStart(misses);
        });
    },
    confirmStart(misses: number) {
      API.get('/healthcheck')
        .then((res: AxiosResponse) => {
          if (asRecord(res.data)?.status === 'healthy') {
            this.fixLost = false;
            this.afterDatabase('done');
            return;
          }
          this.retryDatabaseFix(misses);
        })
        .catch(() => {
          this.retryDatabaseFix(misses);
        });
    },
    retryDatabaseFix(misses: number) {
      if (misses + 1 >= fixConfirmTries) {
        this.fixLost = true;
        this.fixPoll = setTimeout(() => this.confirmStart(misses + 1), fixLostInterval);
        return;
      }
      this.fixPoll = setTimeout(() => this.pollDatabaseFix(misses + 1), fixPollInterval);
    },
    afterDatabase(phase: unknown) {
      if (phase === 'done') {
        this.$toast.add({ severity: 'success', summary: 'Success', detail: 'DMRHub reached its database and is starting', life: 5000 });
        setTimeout(() => {
          window.location.href = '/';
        }, 3000);
        return;
      }
      this.$toast.add({
        severity: 'success', summary: 'Success', detail: 'DMRHub reached its database. Check the other settings and save.', life: 5000,
      });
      this.load();
    },
  },
});
</script>
