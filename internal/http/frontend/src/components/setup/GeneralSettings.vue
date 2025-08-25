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
      <template #title>General Settings</template>
      <template #content>
        <p>General application settings</p>
        <br />
        <span class="p-float-label">
          <InputText
            id="network-name"
            type="text"
            v-model="networkName"
            :class="{ 'p-invalid': (errors && errors['network-name']) || false }"
          />
          <label for="network-name">Network Name</label>
        </span>
        <p>
          The name of the DMR network. This is used in various places throughout the application.
        </p>
        <span v-if="errors && errors['network-name']" class="p-error">{{ errors['network-name'] }}</span>
        <br />
        <span class="p-float-label">
          <Dropdown id="log-level" v-model="logLevel" optionValue="value" optionLabel="label" :options="[
            { label: 'Debug', value: 'debug' },
            { label: 'Info', value: 'info' },
            { label: 'Warn', value: 'warn' },
            { label: 'Error', value: 'error' },
          ]" :class="{ 'p-invalid': (errors && errors['log-level']) || false }" />
          <label for="log-level">Log Level</label>
        </span>
        <p>
          The log level for the application. One of <code>debug</code>, <code>info</code>,
          <code>warn</code>, or <code>error</code>.
        </p>
        <span v-if="errors && errors['log-level']" class="p-error">{{ errors['log-level'] }}</span>
        <br />
        <span class="p-float-label">
          <InputText
            id="secret"
            type="password"
            v-model="secret"
            :class="{ 'p-invalid': (errors && errors.secret) || false }"
          />
          <label for="secret">Secret</label>
        </span>
        <p>
          The secret used to sign session cookies. This should be a long, random string.
        </p>
        <span v-if="errors && errors.secret" class="p-error">{{ errors.secret }}</span>
        <br />
        <span class="p-float-label">
          <InputText
            id="password-salt"
            type="password"
            v-model="passwordSalt"
            :class="{ 'p-invalid': (errors && errors['password-salt']) || false }"
          />
          <label for="password-salt">Password Salt</label>
        </span>
        <p>
          The salt used to hash user passwords in the database. This should be a long, random string.
        </p>
        <span v-if="errors && errors['password-salt']" class="p-error">{{ errors['password-salt'] }}</span>
        <br />
        <span class="p-float-label">
          <InputText
            id="hibp-api-key"
            type="password"
            v-model="hibpApiKey"
            :class="{ 'p-invalid': (errors && errors['hibp-api-key']) || false }"
          />
          <label for="hibp-api-key">HaveIBeenPwned API Key</label>
        </span>
        <p>
          The API key to use when querying the HaveIBeenPwned API to check for compromised passwords.
          If empty, password checks are disabled.
        </p>
        <span v-if="errors && errors['hibp-api-key']" class="p-error">{{ errors['hibp-api-key'] }}</span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import InputText from 'primevue/inputtext';
import Dropdown from 'primevue/dropdown';

export default {
  components: {
    Card,
    InputText,
    Dropdown: Dropdown,
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
    networkName: {
      get() {
        return (this.modelValue && this.modelValue['network-name']) || '';
      },
      set(value) {
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
      set(value) {
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
      set(value) {
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
      set(value) {
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
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'hibp-api-key': value,
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
