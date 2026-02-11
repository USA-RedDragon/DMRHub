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
      <template #title>Email Settings</template>
      <template #content>
        <p>DMRHub can send email notifications to admins when:
        <ul>
          <li>A new user registers</li>
          <li>A user is promoted to admin</li>
          <li>A user is demoted from admin</li>
        </ul>
        </p>
        <br />
        <span>
          <Checkbox id="enabled" inputId="enabled" v-model="enabled" :binary="true" />
          <label for="enabled">&nbsp;&nbsp;Enabled</label>
        </span>
        <br v-if="enabled" />
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="from" type="text" v-model="from" :class="{ 'p-invalid': (errors && errors.from) || false }" />
          <label for="from">From Address</label>
        </span>
        <p v-if="enabled">The email address used as the From address in email notifications</p>
        <span v-if="enabled && errors && errors.from" class="p-error">{{ errors.from }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <Dropdown id="authMethod" v-model="authMethod" optionValue="value" optionLabel="label" :options="[
            { label: 'Plain', value: 'plain' },
            { label: 'Login', value: 'login' },
            { label: 'None', value: 'none' },
          ]" :class="{ 'p-invalid': (errors && errors['auth-method']) || false }" />
          <label for="authMethod">Authentication Method</label>
        </span>
        <p v-if="enabled">
          The authentication method to use when connecting to the SMTP server.
          One of <code>plain</code>, <code>login</code>, or <code>none</code>.
        </p>
        <span v-if="enabled && errors && errors['auth-method']" class="p-error">{{ errors['auth-method'] }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="host" type="text" v-model="host" :class="{ 'p-invalid': (errors && errors.host) || false }" />
          <label for="host">Host</label>
        </span>
        <p v-if="enabled">
          The hostname or IP address of the SMTP server to connect to.
        </p>
        <span v-if="enabled && errors && errors.host" class="p-error">{{ errors.host }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="port" type="number" v-model="port"
            :class="{ 'p-invalid': (errors && errors.port) || false }" />
          <label for="port">Port</label>
        </span>
        <p v-if="enabled">
          The port number of the SMTP server to connect to.
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="username" type="text" v-model="username"
            :class="{ 'p-invalid': (errors && errors.username) || false }" />
          <label for="username">Username</label>
        </span>
        <p v-if="enabled">
          The username to use when connecting to the SMTP server.
        </p>
        <span v-if="enabled && errors && errors.username" class="p-error">{{ errors.username }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="password" type="password" v-model="password"
            :class="{ 'p-invalid': (errors && errors.password) || false }" />
          <label for="password">Password</label>
        </span>
        <p v-if="enabled">
          The password to use when connecting to the SMTP server.
        </p>
        <small v-if="enabled" class="p-text-secondary">{{ passwordStatusMessage }}</small>
        <span v-if="enabled && errors && errors.password" class="p-error">{{ errors.password }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <Dropdown id="tls" v-model="tls" optionValue="value" optionLabel="label" :options="[
            { label: 'None', value: 'none' },
            { label: 'STARTTLS', value: 'starttls' },
            { label: 'Implicit', value: 'implicit' },
          ]" :class="{ 'p-invalid': (errors && errors.tls) || false }" />
          <label for="tls">TLS Mode</label>
        </span>
        <p v-if="enabled">
          The TLS mode to use when connecting to the SMTP server. One of
          <code>none</code>, <code>starttls</code>, or <code>implicit</code>.
        </p>
        <span v-if="enabled && errors && errors.tls" class="p-error">{{ errors.tls }}</span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import Checkbox from 'primevue/checkbox';
import InputText from 'primevue/inputtext';
import Dropdown from 'primevue/dropdown';

export default {
  components: {
    Card,
    Checkbox,
    InputText,
    Dropdown,
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
        smtpPasswordSet: false,
      }),
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
    from: {
      get() {
        return (this.modelValue && this.modelValue.from) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'from': value,
        });
      },
    },
    tls: {
      get() {
        return (this.modelValue && this.modelValue.tls) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'tls': value,
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
    authMethod: {
      get() {
        return (this.modelValue && this.modelValue['auth-method']) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'auth-method': value,
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
    passwordStatusMessage() {
      if (this.password !== '') {
        return this.secretStatus.smtpPasswordSet
          ? 'Will replace the stored value when you save.'
          : 'Will be saved when you submit.';
      }
      return this.secretStatus.smtpPasswordSet
        ? 'Stored. Leave blank to keep the existing value.'
        : 'Not set.';
    },
  },
  data: function () {
    return {};
  },
  mounted() { },
};
</script>
