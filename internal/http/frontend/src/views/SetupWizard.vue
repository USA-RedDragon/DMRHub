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
    <form @submit.prevent="submit()">
      <GeneralSettings v-model="config" :errors="errors" :secret-status="secretStatus" />
      <br />
      <DMRSettings v-model="config.dmr" :errors="errors.dmr" />
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
      <div class="card-footer">
        <ShadButton type="submit" variant="outline" size="sm">Save</ShadButton>
      </div>
    </form>
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

import API from '@/services/API';

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

export default defineComponent({
  components: {
    ShadButton,
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
  created() {
    this.getConfig();
  },
  mounted() { },
  unmounted() { },
  data: function () {
    return {
      config: createInitialConfig() as SetupConfig,
      errors: createInitialErrors() as SetupErrors,
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
  },
  methods: {
    setupWizardHeaders(): Record<string, string> {
      const token = this.$route.query.token;
      if (typeof token === 'string' && token.length > 0) {
        return { 'X-SetupWizard-Token': token };
      }

      if (Array.isArray(token) && token.length > 0 && token[0]) {
        return { 'X-SetupWizard-Token': token[0] };
      }

      return {};
    },
    getConfig() {
      API.get('/config', { headers: this.setupWizardHeaders() })
        .then((response: AxiosResponse) => {
          const payload = response.data && response.data.config ? response.data.config : response.data;
          this.secretStatus = response.data && response.data.secrets ? response.data.secrets : this.secretStatus;
          if (payload && payload.secrets) {
            delete payload.secrets;
          }
          this.config = {
            ...createInitialConfig(),
            ...(asRecord(payload) || {}),
          };
        })
        .catch((error: unknown) => {
          console.log(error);
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
      return payload;
    },
    async checkConfig(config: Record<string, unknown>, showToast: boolean): Promise<boolean> {
      const response = await API.post(
        '/config/validate',
        config,
        { headers: this.setupWizardHeaders() },
      );
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
      console.error('submit');
      const payload = this.buildConfigPayload();
      if (!await this.checkConfig(payload, true)) {
        return;
      }
      try {
        await API.put('/config', payload, { headers: this.setupWizardHeaders() });
        this.$toast.add({
          severity: 'success',
          summary: 'Success',
          detail: 'Configuration saved successfully',
          life: 3000,
        });
        if (!this.isAdminSetup) {
          const queryToken = this.$route.query.token;
          setTimeout(() => {
            this.$router.push({
              path: '/setup/user',
              query: {
                token: typeof queryToken === 'string' ? queryToken : undefined,
              },
            });
          }, 500);
        }
      } catch (error: unknown) {
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
