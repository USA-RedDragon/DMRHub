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
    <Card>
      <CardHeader>
        <CardTitle>Database Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>DMRHub supports multiple database backends. For small deployments or testing, SQLite is recommended.
          For larger deployments or when high availability is required, PostgreSQL or MySQL/MariaDB is required.</p>
        <br />
        <label class="field-label" for="driver">Driver</label>
        <select id="driver" v-model="driver" class="ui-select" :class="{ 'ui-select-invalid': (errors && errors.driver) || false }">
          <option v-for="option in [
            { label: 'SQLite', value: 'sqlite' },
            { label: 'PostgreSQL', value: 'postgres' },
            { label: 'MySQL', value: 'mysql' },
          ]" :key="option.value" :value="option.value">{{ option.label }}</option>
        </select>
        <p>
          The database driver to use. One of <code>sqlite</code>, <code>postgres</code>, or <code>mysql</code>.
        </p>
        <span v-if="errors && errors.driver" class="p-error">{{ errors.driver }}</span>
        <br />
        <label class="field-label" for="database">Database</label>
        <ShadInput
          id="database"
          type="text"
          v-model="database"
          :aria-invalid="(errors && errors.database) || false"
        />
        <p>
          The database name or path to the SQLite file.
        </p>
        <span v-if="errors && errors.database" class="p-error">{{ errors.database }}</span>
        <br v-if="driver !== 'sqlite'" />
        <label class="field-label" for="host" v-if="driver !== 'sqlite'">Host</label>
        <ShadInput id="host" type="text" v-model="host" v-if="driver !== 'sqlite'" :aria-invalid="(errors && errors.host) || false" />
        <p v-if="driver !== 'sqlite'">
          The hostname or IP address of the database server.
        </p>
        <span v-if="driver !== 'sqlite' && errors && errors.host" class="p-error">{{ errors.host }}</span>
        <br v-if="driver !== 'sqlite'" />
        <label class="field-label" for="port" v-if="driver !== 'sqlite'">Port</label>
        <ShadInput
          id="port"
          type="number"
          v-model="port"
          v-if="driver !== 'sqlite'"
          :aria-invalid="(errors && errors.port) || false"
        />
        <p v-if="driver !== 'sqlite'">
          The port number of the database server.
        </p>
        <span v-if="driver !== 'sqlite' && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="driver !== 'sqlite'" />
        <label class="field-label" for="username" v-if="driver !== 'sqlite'">Username</label>
        <ShadInput
          id="username"
          type="text"
          v-model="username"
          v-if="driver !== 'sqlite'"
          :aria-invalid="(errors && errors.username) || false"
        />
        <p v-if="driver !== 'sqlite'">
          The username to use when connecting to the database server.
        </p>
        <span v-if="driver !== 'sqlite' && errors && errors.username" class="p-error">{{ errors.username }}</span>
        <br v-if="driver !== 'sqlite'" />
        <label class="field-label" for="password" v-if="driver !== 'sqlite'">Password</label>
        <ShadInput
          id="password"
          type="password"
          v-model="password"
          v-if="driver !== 'sqlite'"
          :aria-invalid="(errors && errors.password) || false"
        />
        <p v-if="driver !== 'sqlite'">
          The password to use when connecting to the database server.
        </p>
        <span v-if="driver !== 'sqlite' && errors && errors.password" class="p-error">{{ errors.password }}</span>
        <br />
        <label class="field-label" for="extraParameters">Extra Parameters</label>
        <textarea
          rows="5"
          id="extraParameters"
          v-model="extraParameters"
          class="ui-textarea"
          :class="{ 'ui-textarea-invalid': (errors && errors['extra-parameters']) || false }"
        />
        <p>
          Extra connection parameters to pass to the database driver. One per line.
        </p>
        <span v-if="errors && errors['extra-parameters']" class="p-error">{{ errors['extra-parameters'] }}</span>
      </CardContent>
    </Card>
  </div>
</template>

<script lang="ts">
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';

export default {
  components: {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    ShadInput,
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
    driver: {
      get() {
        return (this.modelValue && this.modelValue.driver) || '';
      },
      set(value: string) {
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
      set(value: string) {
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
      set(value: string) {
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
      set(value: string) {
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
      set(value: string) {
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
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'password': value,
        });
      },
    },
    extraParameters: {
      get() {
        const extraParameters = this.modelValue && this.modelValue['extra-parameters'];
        return Array.isArray(extraParameters) ? extraParameters.join('\n') : '';
      },
      set(value: string) {
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

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}

.ui-select,
.ui-textarea {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}

.ui-select-invalid,
.ui-textarea-invalid {
  border-color: var(--primary);
}
</style>
