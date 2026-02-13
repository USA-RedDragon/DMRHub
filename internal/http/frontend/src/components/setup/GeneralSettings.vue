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
        <CardTitle>General Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>General application settings</p>
        <br />
        <label class="field-label" for="network-name">Network Name</label>
        <ShadInput id="network-name" type="text" v-model="networkName"
          :aria-invalid="(errors && errors['network-name']) || false" />
        <p>
          The name of the DMR network. This is used in various places throughout the application.
        </p>
        <span v-if="errors && errors['network-name']" class="p-error">{{ errors['network-name'] }}</span>
        <br />
        <label class="field-label" for="log-level">Log Level</label>
        <select id="log-level" v-model="logLevel" class="ui-select" :class="{ 'ui-select-invalid': (errors && errors['log-level']) || false }">
          <option v-for="option in [
            { label: 'Debug', value: 'debug' },
            { label: 'Info', value: 'info' },
            { label: 'Warn', value: 'warn' },
            { label: 'Error', value: 'error' },
          ]" :key="option.value" :value="option.value">{{ option.label }}</option>
        </select>
        <p>
          The log level for the application. One of <code>debug</code>, <code>info</code>,
          <code>warn</code>, or <code>error</code>.
        </p>
        <span v-if="errors && errors['log-level']" class="p-error">{{ errors['log-level'] }}</span>
        <br />
        <label class="field-label" for="secret">Secret</label>
        <ShadInput id="secret" type="password" v-model="secret"
          :aria-invalid="(errors && errors.secret) || false" />
        <p>
          The secret used to sign session cookies. This should be a long, random string.
        </p>
        <small class="p-text-secondary">{{ secretStatusMessage }}</small>
        <span v-if="errors && errors.secret" class="p-error">{{ errors.secret }}</span>
        <br />
        <br />
        <label class="field-label" for="password-salt">Password Salt</label>
        <ShadInput id="password-salt" type="password" v-model="passwordSalt"
          :aria-invalid="(errors && errors['password-salt']) || false" />
        <p>
          The salt used to hash user passwords in the database. This should be a long, random string.
        </p>
        <small class="p-text-secondary">{{ passwordSaltStatusMessage }}</small>
        <span v-if="errors && errors['password-salt']" class="p-error">{{ errors['password-salt'] }}</span>
        <br />
        <br />
        <label class="field-label" for="hibp-api-key">HaveIBeenPwned API Key</label>
        <ShadInput id="hibp-api-key" type="password" v-model="hibpApiKey"
          :aria-invalid="(errors && errors['hibp-api-key']) || false" />
        <p>
          The API key to use when querying the HaveIBeenPwned API to check for compromised passwords.
          If empty, password checks are disabled.
        </p>
        <span v-if="errors && errors['hibp-api-key']" class="p-error">{{ errors['hibp-api-key'] }}</span>
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
    secretStatus: {
      type: Object,
      required: false,
      default: () => ({
        secretSet: false,
        passwordSaltSet: false,
      }),
    },
  },
  emits: ['update:modelValue'],
  computed: {
    networkName: {
      get() {
        return (this.modelValue && this.modelValue['network-name']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'network-name': value,
        });
      },
    },
    logLevel: {
      get() {
        return (this.modelValue && this.modelValue['log-level']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'log-level': value,
        });
      },
    },
    secret: {
      get() {
        return (this.modelValue && this.modelValue['secret']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'secret': value,
        });
      },
    },
    passwordSalt: {
      get() {
        return (this.modelValue && this.modelValue['password-salt']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'password-salt': value,
        });
      },
    },
    hibpApiKey: {
      get() {
        return (this.modelValue && this.modelValue['hibp-api-key']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'hibp-api-key': value,
        });
      },
    },
    secretStatusMessage() {
      if (this.secret !== '') {
        return this.secretStatus.secretSet
          ? 'Will replace the stored value when you save.'
          : 'Will be saved when you submit.';
      }
      return this.secretStatus.secretSet
        ? 'Stored. Leave blank to keep the existing value.'
        : 'Not set. Required.';
    },
    passwordSaltStatusMessage() {
      if (this.passwordSalt !== '') {
        return this.secretStatus.passwordSaltSet
          ? 'Will replace the stored value when you save.'
          : 'Will be saved when you submit.';
      }
      return this.secretStatus.passwordSaltSet
        ? 'Stored. Leave blank to keep the existing value.'
        : 'Not set. Required.';
    },
  },
  data: function () {
    return {};
  },
  mounted() { },
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}

.ui-select {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}

.ui-select-invalid {
  border-color: var(--primary);
}
</style>
