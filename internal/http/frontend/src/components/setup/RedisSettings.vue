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
      <CardHeader>
        <CardTitle>Redis Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>Redis should be used in cases where DMRHub needs to be scaled horizontally.
          Without Redis, DMRHub can only run on a single instance.</p>
        <br />
        <div class="checkbox-row">
          <input
            id="enabled"
            type="checkbox"
            v-model="enabled"
          />
          <label for="enabled">Enabled</label>
        </div>
        <br v-if="enabled" />
        <br v-if="enabled" />
        <label class="field-label" for="host" v-if="enabled">Host</label>
        <ShadInput id="host" type="text" v-model="host" v-if="enabled" :aria-invalid="(errors && errors.host) || false" />
        <p v-if="enabled">
          The hostname or IP address of the Redis server to connect to.
        </p>
        <span v-if="enabled && errors && errors.host" class="p-error">{{ errors.host }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="port" v-if="enabled">Port</label>
        <ShadInput
          id="port"
          type="number"
          v-model="port"
          v-if="enabled"
          :aria-invalid="(errors && errors.port) || false"
        />
        <p v-if="enabled">
          The port number of the Redis server to connect to.
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="password" v-if="enabled">Password</label>
        <ShadInput
          id="password"
          type="password"
          v-model="password"
          v-if="enabled"
          :aria-invalid="(errors && errors.password) || false"
        />
        <p v-if="enabled">
          The password to use when connecting to the Redis server.
        </p>
        <span v-if="enabled && errors && errors.password" class="p-error">{{ errors.password }}</span>
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
    enabled: {
      get() {
        return (this.modelValue && this.modelValue.enabled) || false;
      },
      set(value: boolean) {
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

.checkbox-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
