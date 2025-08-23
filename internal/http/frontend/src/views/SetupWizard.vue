<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2024 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https:  www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <div>
    <PVToast />
    <form @submit.prevent="submit()">
      <Card>
        <template #title>Setup</template>
        <template #content>
            <GeneralSettings v-model="config" :errors="errors" />
            <br />
            <DMRSettings v-model="config.dmr" :errors="errors.dmr" />
            <br />
            <HTTPSettings v-model="config.http" :errors="errors.http" />
            <br />
            <DatabaseSettings v-model="config.database" :errors="errors.database" />
            <br />
            <SMTPSettings v-model="config.smtp" :errors="errors.smtp" />
            <br />
            <RedisSettings v-model="config.redis" :errors="errors.redis" />
            <br />
            <MetricsSettings v-model="config.metrics" :errors="errors.metrics" />
            <br />
            <PProfSettings v-model="config.pprof" :errors="errors.pprof" />
        </template>
        <template #footer>
          <div class="card-footer">
            <PVButton
              class="p-button-raised p-button-rounded"
              icon="pi pi-check"
              label="&nbsp;Save"
              type="submit"
            />
          </div>
        </template>
      </Card>
    </form>
  </div>
</template>

<script>
import Card from 'primevue/card';
import GeneralSettings from '@/components/setup/GeneralSettings.vue';
import RedisSettings from '@/components/setup/RedisSettings.vue';
import DatabaseSettings from '@/components/setup/DatabaseSettings.vue';
import HTTPSettings from '@/components/setup/HTTPSettings.vue';
import DMRSettings from '@/components/setup/DMRSettings.vue';
import SMTPSettings from '@/components/setup/SMTPSettings.vue';
import MetricsSettings from '@/components/setup/MetricsSettings.vue';
import PProfSettings from '@/components/setup/PProfSettings.vue';

import Toast from 'primevue/toast';
import Button from 'primevue/button';

import API from '@/services/API';

export default {
  components: {
    Card,
    PVToast: Toast,
    PVButton: Button,
    DatabaseSettings,
    HTTPSettings,
    DMRSettings,
    SMTPSettings,
    MetricsSettings,
    PProfSettings,
    GeneralSettings,
    RedisSettings,
  },
  head: {
    title: 'Setup',
  },
  created() {
    this.getConfig();
  },
  mounted() {},
  unmounted() {},
  data: function() {
    return {
      config: {},
      errors: [],
    };
  },
  methods: {
    getConfig() {
      API.get('/config')
        .then((response) => {
          this.config = response.data;
          this.checkConfig(this.config);
        })
        .catch((error) => {
          console.log(error);
        });
    },
    async checkConfig(config) {
      const response = await API.post('/config/validate', config);
      if (response.data.valid) {
        return true;
      } else {
        this.errors = response.data.errors;
        this.$toast.add({
          severity: 'error',
          summary: 'Error',
          detail: 'Configuration is invalid. Please correct the errors and try again.',
          life: 3000,
        });
        return false;
      }
    },
    async submit() {
      console.error('submit');
      if (!await this.checkConfig(this.config)) {
        return;
      }
      try {
        await API.put('/config', this.config);
        this.$toast.add({
          severity: 'success',
          summary: 'Success',
          detail: 'Configuration saved successfully',
          life: 3000,
        });
      } catch (error) {
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
};
</script>

<style scoped></style>
