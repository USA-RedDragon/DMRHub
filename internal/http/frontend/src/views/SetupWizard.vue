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
  <div>
    <h1>Setup</h1>
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
          for the amber mark below.
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
      <form @submit.prevent="submit()">
        <GeneralSettings v-model="config" :errors="errors" :secret-status="secretStatus" />
        <br />
        <DMRSettings
          v-model="config.dmr"
          :errors="errors.dmr"
          :secret-status="secretStatus"
          :show-transcoding-status="isAdminSetup"
          :saves="saves"
        />
        <br />
        <HTTPSettings v-model="config.http" :errors="errors.http" />
        <br />
        <DatabaseSettings v-model="config.database" :errors="errors.database" />
        <br />
        <SMTPSettings v-model="config.smtp" :errors="errors.smtp" :secret-status="secretStatus" />
        <br />
        <RedisSettings v-model="config.redis" :errors="errors.redis" />
        <br />
        <MetricsSettings v-model="config.metrics" :errors="errors.metrics" />
        <br />
        <PProfSettings v-model="config.pprof" :errors="errors.pprof" />
        <br v-if="setupState.replicas.length > 1" />
        <ReplicasCard v-if="setupState.replicas.length > 1" :replicas="setupState.replicas" :revision="setupState.revision" />
        <div class="card-footer mt-4 flex flex-wrap items-center gap-3">
          <ShadButton type="submit" variant="outline" size="sm">Save</ShadButton>
          <small v-if="setupState.replicas.length > 1" class="text-muted-foreground">
            Saving puts the settings into effect on every replica at once, without a restart.
          </small>
        </div>
      </form>
    </template>
  </div>
</template>

<script lang="ts">
import type { AxiosResponse } from 'axios';
import { defineComponent } from 'vue';

import { Button as ShadButton } from '@/components/ui/button';

import GeneralSettings from '@/components/setup/GeneralSettings.vue';
import RedisSettings from '@/components/setup/RedisSettings.vue';
import DatabaseSettings from '@/components/setup/DatabaseSettings.vue';
import HTTPSettings from '@/components/setup/HTTPSettings.vue';
import DMRSettings from '@/components/setup/DMRSettings.vue';
import SMTPSettings from '@/components/setup/SMTPSettings.vue';
import MetricsSettings from '@/components/setup/MetricsSettings.vue';
import PProfSettings from '@/components/setup/PProfSettings.vue';

import ReplicasCard from '@/components/setup/ReplicasCard.vue';
import API from '@/services/API';
import { ambeServersError, ambeServersPayload, type AmbeServerRow } from '@/lib/codec';
import {
  emptySetupState, replicaName, serverName, settingLabel, setupStateKey, setupStateOf, stateKeys, type SetupState,
} from '@/lib/setupState';

type SetupSection = Record<string, unknown>;

type SetupConfig = {
  dmr: SetupSection;
  http: SetupSection;
  database: SetupSection;
  smtp: SetupSection;
  redis: SetupSection;
  metrics: SetupSection;
  pprof: SetupSection;
  [key: string]: unknown;
};

type SetupErrors = {
  dmr: SetupSection;
  http: SetupSection;
  database: SetupSection;
  smtp: SetupSection;
  redis: SetupSection;
  metrics: SetupSection;
  pprof: SetupSection;
  [key: string]: unknown;
};

type SecretStatus = {
  secretSet: boolean;
  passwordSaltSet: boolean;
  smtpPasswordSet: boolean;
};

const createInitialConfig = (): SetupConfig => ({
  dmr: {},
  http: {},
  database: {},
  smtp: {},
  redis: {},
  metrics: {},
  pprof: {},
});

const createInitialErrors = (): SetupErrors => ({
  dmr: {},
  http: {},
  database: {},
  smtp: {},
  redis: {},
  metrics: {},
  pprof: {},
});

const asRecord = (value: unknown): Record<string, unknown> | undefined => {
  if (typeof value === 'object' && value !== null) {
    return value as Record<string, unknown>;
  }

  return undefined;
};

type ApplyError = {
  replica?: string;
  server: string;
  field?: string;
  running: boolean;
  error: string;
};

type SaveReplica = {
  id: string;
  self: boolean;
  answered: boolean;
};

type ShownApplyError = {
  summary: string;
  detail: string;
};

type SaveFailure = {
  applyErrors: ApplyError[];
  replicas: SaveReplica[];
};

const saveFailureOf = (error: unknown): SaveFailure | undefined => {
  const response = asRecord(asRecord(error)?.response);
  const data = asRecord(response?.data);
  if (response?.status !== 409 || !Array.isArray(data?.applyErrors)) {
    return undefined;
  }
  return {
    applyErrors: data.applyErrors as ApplyError[],
    replicas: Array.isArray(data.replicas) ? (data.replicas as SaveReplica[]) : [],
  };
};

const replicasOf = (data: unknown): SaveReplica[] => {
  const replicas = asRecord(data)?.replicas;
  return Array.isArray(replicas) ? (replicas as SaveReplica[]) : [];
};

const listNames = (names: string[]): string => {
  if (names.length <= 1) {
    return names.join('');
  }
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
};

const ambeServerRows = (config: Record<string, unknown>): AmbeServerRow[] | undefined => {
  const servers = asRecord(asRecord(config.dmr)?.transcoding)?.['ambe-servers'];
  return Array.isArray(servers) ? (servers as AmbeServerRow[]) : undefined;
};

const localErrors = (config: Record<string, unknown>): SetupErrors | undefined => {
  const error = ambeServersError(ambeServerRows(config) ?? []);
  if (!error) {
    return undefined;
  }
  return { ...createInitialErrors(), dmr: { transcoding: { 'ambe-servers': error } } };
};

type ClosedReason = '' | 'database' | 'settings';

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
  const name = serverName(e.server);
  const state = e.running ? 'It keeps running with its previous settings.' : 'It is not running.';
  const of = self || !replica ? '' : ` of replica ${replica}`;
  return {
    summary: `The ${name} server${of} could not start with the new settings. ${state}`,
    detail: e.error,
  };
};

export default defineComponent({
  components: {
    ShadButton,
    ReplicasCard,
    DatabaseSettings,
    HTTPSettings,
    DMRSettings,
    SMTPSettings,
    MetricsSettings,
    PProfSettings,
    GeneralSettings,
    RedisSettings,
  },
  head() {
    return {
      title: this.isAdminSetup ? 'Admin Setup' : 'Setup',
    };
  },
  provide() {
    return { [setupStateKey as symbol]: this.setupState };
  },
  created() {
    if (this.isAdminSetup) {
      this.getConfig();
      return;
    }
    API.get('/setupwizard')
      .then((res: AxiosResponse) => {
        if (closedReasonOf(res.data)) {
          this.closed = closedReasonOf(res.data);
          return;
        }
        if (res.data?.config_valid && res.data?.first_admin_open) {
          this.$router.replace('/setup/user');
        }
        this.getConfig();
      })
      .catch(() => {
        this.getConfig();
      });
  },
  mounted() { },
  unmounted() { },
  data: function () {
    return {
      config: createInitialConfig() as SetupConfig,
      errors: createInitialErrors() as SetupErrors,
      applyErrors: [] as ShownApplyError[],
      failedReplicas: [] as string[],
      applyOK: '',
      unanswered: [] as string[],
      setupState: emptySetupState() as SetupState,
      closed: '' as ClosedReason,
      saves: 0,
      secretStatus: {
        secretSet: false,
        passwordSaltSet: false,
        smtpPasswordSet: false,
      } as SecretStatus,
    };
  },
  computed: {
    isAdminSetup() {
      return this.$route.path.startsWith('/admin/setup');
    },
    databasePhase(): boolean {
      return this.setupState.wizard?.phase === 'database';
    },
    databaseAdvice(): string {
      const w = this.setupState.wizard;
      if (w?.database_locked) {
        return 'The deployment sets the database. Fix it in the config file or the environment, then restart DMRHub.';
      }
      if (w?.store_error) {
        return `The setup wizard cannot keep a database setting here: ${w.store_error}. Set the database in the config file or the environment of the deployment, then restart DMRHub.`;
      }
      return `Fix the database settings below and save. DMRHub tries them at once and keeps them in ${w?.database_file ?? 'its working directory'}. The other settings open once DMRHub reaches its database.`;
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
      const names = listNames(this.unanswered.map(replicaName));
      return `Replica ${names} did not report in time. The Replicas table shows its state once it reports.`;
    },
  },
  methods: {
    afterDatabase(phase: unknown) {
      if (phase === 'done') {
        this.$toast.add({
          severity: 'success',
          summary: 'Success',
          detail: 'DMRHub reached its database and is starting',
          life: 5000,
        });
        setTimeout(() => {
          window.location.href = '/';
        }, 3000);
        return;
      }
      this.$toast.add({
        severity: 'success',
        summary: 'Success',
        detail: 'DMRHub reached its database. Check the other settings and save.',
        life: 5000,
      });
      this.getConfig();
    },
    getConfig() {
      API.get('/config')
        .then((response: AxiosResponse) => {
          const payload = response.data && response.data.config ? response.data.config : response.data;
          this.secretStatus = response.data && response.data.secrets ? response.data.secrets : this.secretStatus;
          Object.assign(this.setupState, setupStateOf(response.data));
          for (const key of stateKeys) {
            if (payload && key in payload) {
              delete payload[key];
            }
          }
          this.config = {
            ...createInitialConfig(),
            ...(asRecord(payload) || {}),
          };
        })
        .catch((error: unknown) => {
          const closed = closedReasonOf(asRecord(asRecord(error)?.response)?.data);
          if (closed && !this.isAdminSetup) {
            this.closed = closed;
            return;
          }
          console.log(error);
          this.$toast.add({
            severity: 'error',
            summary: 'Error',
            detail: 'Failed to load configuration',
            life: 3000,
          });
        });
    },
    buildConfigPayload(): Record<string, unknown> {
      const payload = JSON.parse(JSON.stringify(this.config || {}));
      if (payload.secret === '') {
        delete payload.secret;
      }
      if (payload['password-salt'] === '') {
        delete payload['password-salt'];
      }
      const smtp = asRecord(payload.smtp);
      if (smtp && smtp.password === '') {
        delete smtp.password;
      }
      const database = asRecord(payload.database);
      if (database && database.password === '') {
        delete database.password;
      }
      const redis = asRecord(payload.redis);
      if (redis && redis.password === '') {
        delete redis.password;
      }
      const transcoding = asRecord(asRecord(payload.dmr)?.transcoding);
      const servers = ambeServerRows(payload);
      if (transcoding && servers) {
        transcoding['ambe-servers'] = ambeServersPayload(servers);
      }
      return payload;
    },
    checkLocal(showToast: boolean): boolean {
      const local = localErrors(this.config);
      if (!local) {
        return true;
      }
      this.errors = local;
      if (showToast) {
        this.$toast.add({
          severity: 'error',
          summary: 'Error',
          detail: 'Configuration is invalid. Please correct the errors and try again.',
          life: 3000,
        });
      }
      return false;
    },
    async checkConfig(config: Record<string, unknown>, showToast: boolean): Promise<boolean> {
      const response = await API.post('/config/validate', config);
      if (response.data.valid) {
        this.errors = createInitialErrors();
        return true;
      } else {
        this.errors = response.data.errors;
        if (showToast) {
          this.$toast.add({
            severity: 'error',
            summary: 'Error',
            detail: 'Configuration is invalid. Please correct the errors and try again.',
            life: 3000,
          });
        }
        return false;
      }
    },
    async submit() {
      if (!this.checkLocal(true)) {
        return;
      }
      const payload = this.buildConfigPayload();
      try {
        if (!await this.checkConfig(payload, true)) {
          return;
        }
        const response = await API.put('/config', payload);
        this.saves++;
        this.applyErrors = [];
        this.failedReplicas = [];
        this.applyOK = '';
        this.unanswered = replicasOf(response?.data).filter((r) => !r.self && !r.answered).map((r) => r.id);
        if (this.databasePhase) {
          this.afterDatabase(response?.data?.phase);
          return;
        }
        this.getConfig();
        this.$toast.add({
          severity: 'success',
          summary: 'Success',
          detail: 'Configuration saved successfully',
          life: 3000,
        });
        if (!this.isAdminSetup) {
          setTimeout(() => {
            this.$router.push('/setup/user');
          }, 500);
        }
      } catch (error: unknown) {
        const failure = saveFailureOf(error);
        if (failure) {
          this.saves++;
          const self = failure.replicas.find((r) => r.self)?.id;
          this.applyErrors = failure.applyErrors.map((e) => showApplyError(e, !e.replica || e.replica === self));
          this.failedReplicas = [...new Set(failure.applyErrors.map((e) => e.replica ?? ''))]
            .filter((r) => r !== '' && r !== self);
          const selfFailed = failure.applyErrors.some((e) => !e.replica || e.replica === self);
          this.applyOK = self && !selfFailed && this.failedReplicas.length > 0
            ? `Replica ${replicaName(self)} runs with the new settings.`
            : '';
          this.unanswered = failure.replicas.filter((r) => !r.self && !r.answered).map((r) => r.id);
          this.getConfig();
          this.$toast.add({
            severity: 'error',
            summary: 'Error',
            detail: 'Configuration saved, but some settings could not be put into effect',
            life: 5000,
          });
          return;
        }
        console.log(error);
        this.$toast.add({
          severity: 'error',
          summary: 'Error',
          detail: 'Failed to save configuration',
          life: 3000,
        });
      }
    },
  },
});
</script>

<style scoped></style>
