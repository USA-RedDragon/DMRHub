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
      <template #title>SMTP Settings</template>
      <template #content>
        <p>SMTP settings</p>
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
          <InputText id="from" type="text" v-model="from" />
          <label for="from">From Address</label>
        </span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="authMethod" type="text" v-model="authMethod" />
          <label for="authMethod">Authentication Method</label>
        </span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="host" type="text" v-model="host" />
          <label for="host">Host</label>
        </span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="port" type="number" v-model="port" />
          <label for="port">Port</label>
        </span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="username" type="text" v-model="username" />
          <label for="username">Username</label>
        </span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="password" type="password" v-model="password" />
          <label for="password">Password</label>
        </span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="tls" type="text" v-model="tls" />
          <label for="tls">TLS Mode</label>
        </span>
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
  },
  data: function() {
    return {};
  },
  mounted() {},
};
</script>
