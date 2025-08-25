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
    <Card>
      <template #title>Redis Settings</template>
      <template #content>
        <p>Redis should be used in cases where DMRHub needs to be scaled horizontally.
          Without Redis, DMRHub can only run on a single instance.</p>
        <br />
        <span>
          <Checkbox
            id="enabled"
            inputId="enabled"
            v-model="enabled"
            :binary="true"
          />
          <label for="enabled">&nbsp;&nbsp;Enabled</label>
        </span>
        <br v-if="enabled" />
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="host" type="text" v-model="host" :class="{ 'p-invalid': (errors && errors.host) || false }" />
          <label for="host">Host</label>
        </span>
        <p v-if="enabled">
          The hostname or IP address of the Redis server to connect to.
        </p>
        <span v-if="enabled && errors && errors.host" class="p-error">{{ errors.host }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText
            id="port"
            type="number"
            v-model="port"
            :class="{ 'p-invalid': (errors && errors.port) || false }"
          />
          <label for="port">Port</label>
        </span>
        <p v-if="enabled">
          The port number of the Redis server to connect to.
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText
            id="password"
            type="password"
            v-model="password"
            :class="{ 'p-invalid': (errors && errors.password) || false }"
          />
          <label for="password">Password</label>
        </span>
        <p v-if="enabled">
          The password to use when connecting to the Redis server.
        </p>
        <span v-if="enabled && errors && errors.password" class="p-error">{{ errors.password }}</span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import Checkbox from 'primevue/checkbox';
import InputText from 'primevue/inputtext';

export default {
  components: {
    Card,
    Checkbox,
    InputText,
  },
  props: {
    modelValue: {
      type: Object,
      required: true,
    },
    errors: {
      type: Object,
      required: true,
    },
  },
  emits: ['update:modelValue'],
  computed: {
    enabled: {
      get() {
        return (this.modelValue && this.modelValue.enabled) || false;
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'enabled': value,
        });
      },
    },
    host: {
      get() {
        return (this.modelValue && this.modelValue.host) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'host': value,
        });
      },
    },
    port: {
      get() {
        return (this.modelValue && this.modelValue.port) || undefined;
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'port': value,
        });
      },
    },
    password: {
      get() {
        return (this.modelValue && this.modelValue.password) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'password': value,
        });
      },
    },
  },
  data: function() {
    return {};
  },
  mounted() {},
};
</script>
