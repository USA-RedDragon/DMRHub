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
    <Card>
      <template #title>Setup</template>
      <template #content>
        <form @submit.prevent="submit()">
          <GeneralSettings v-model="config" />
          <br />
          <RedisSettings v-model="config.redis" />
          <br />
          <DatabaseSettings v-model="config.database" />
          <br />
          <HTTPSettings v-model="config.http" />
          <br />
          <DMRSettings v-model="config.dmr" />
          <br />
          <SMTPSettings v-model="config.smtp" />
          <br />
          <MetricsSettings v-model="config.metrics" />
          <br />
          <PProfSettings v-model="config.pprof" />
        </form>
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
      submitted: false,
    };
  },
  methods: {
    getConfig() {
      API.get('/config')
        .then((response) => {
          this.config = response.data;
          this.checkConfig(response.data);
        })
        .catch((error) => {
          console.log(error);
        });
    },
    checkConfig(config) {
      API.post('/config/validate', config)
        .then((response) => {
          console.log(response.data);
        })
        .catch((error) => {
          console.log(error);
        });
    },
    submit() {
      this.submitted = true;
    },
  },
};
</script>

<style scoped></style>
