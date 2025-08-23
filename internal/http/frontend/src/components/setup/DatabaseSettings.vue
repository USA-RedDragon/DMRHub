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
      <template #title>Database Settings</template>
      <template #content>
        <p>DMRHub supports multiple database backends. For small deployments or testing, SQLite is recommended.
          For larger deployments or when high availability is required, PostgreSQL or MySQL/MariaDB is required.</p>
        <br />
        <span class="p-float-label">
          <Dropdown id="driver" v-model="driver"  optionValue="value" optionLabel="label" :options="[
            { label: 'SQLite', value: 'sqlite' },
            { label: 'PostgreSQL', value: 'postgres' },
            { label: 'MySQL', value: 'mysql' },
          ]" />
          <label for="driver">Driver</label>
        </span>
        <p>
          The database driver to use. One of <code>sqlite</code>, <code>postgres</code>, or <code>mysql</code>.
        </p>
        <br />
        <span class="p-float-label">
          <InputText id="database" type="text" v-model="database" />
          <label for="database">Database</label>
        </span>
        <p>
          The database name or path to the SQLite file.
        </p>
        <br v-if="driver !== 'sqlite'" />
        <span class="p-float-label" v-if="driver !== 'sqlite'">
          <InputText id="host" type="text" v-model="host" />
          <label for="host">Host</label>
        </span>
        <p v-if="driver !== 'sqlite'">
          The hostname or IP address of the database server.
        </p>
        <br v-if="driver !== 'sqlite'" />
        <span class="p-float-label" v-if="driver !== 'sqlite'">
          <InputText id="port" type="number" v-model="port" />
          <label for="port">Port</label>
        </span>
        <p v-if="driver !== 'sqlite'">
          The port number of the database server.
        </p>
        <br v-if="driver !== 'sqlite'" />
        <span class="p-float-label" v-if="driver !== 'sqlite'">
          <InputText id="username" type="text" v-model="username" />
          <label for="username">Username</label>
        </span>
        <p v-if="driver !== 'sqlite'">
          The username to use when connecting to the database server.
        </p>
        <br v-if="driver !== 'sqlite'" />
        <span class="p-float-label" v-if="driver !== 'sqlite'">
          <InputText id="password" type="password" v-model="password" />
          <label for="password">Password</label>
        </span>
        <p v-if="driver !== 'sqlite'">
          The password to use when connecting to the database server.
        </p>
        <br />
        <span class="p-float-label">
          <TextArea rows="5" id="extraParameters" v-model="extraParameters" />
          <label for="extraParameters">Extra Parameters</label>
        </span>
        <p>
          Extra connection parameters to pass to the database driver. One per line.
        </p>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import InputText from 'primevue/inputtext';
import TextArea from 'primevue/textarea';
import Dropdown from 'primevue/dropdown';

export default {
  components: {
    Card,
    InputText,
    TextArea,
    Dropdown,
  },
  props: {
    modelValue: {
      type: Object,
      required: true,
    },
  },
  emits: ['update:modelValue'],
  computed: {
    driver: {
      get() {
        return (this.modelValue && this.modelValue.driver) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'driver': value,
        });
      },
    },
    database: {
      get() {
        return (this.modelValue && this.modelValue.database) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'database': value,
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
    username: {
      get() {
        return (this.modelValue && this.modelValue.username) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'username': value,
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
    extraParameters: {
      get() {
        return (this.modelValue && this.modelValue['extra-parameters'].join('\n')) || [];
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'extra-parameters': value.split('\n'),
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
