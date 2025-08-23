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
          <InputText id="network-name" type="text" v-model="networkName" />
          <label for="network-name">Network Name</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="log-level" type="text" v-model="logLevel" />
          <label for="log-level">Log Level</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="secret" type="password" v-model="secret" />
          <label for="secret">Secret</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="password-salt" type="password" v-model="passwordSalt" />
          <label for="password-salt">Password Salt</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="hibp-api-key" type="password" v-model="hibpApiKey" />
          <label for="hibp-api-key">HaveIBeenPwned API Key</label>
        </span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import InputText from 'primevue/inputtext';

export default {
  components: {
    Card,
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
