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
        <CardTitle>Email Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>DMRHub can send email notifications to admins when:</p>
        <ul>
          <li>A new user registers</li>
          <li>A user is promoted to admin</li>
          <li>A user is demoted from admin</li>
        </ul>
        <br />
        <div class="checkbox-row">
          <input id="smtp-enabled" v-setting="settingLocked('smtp.enabled')" type="checkbox" v-model="enabled" />
          <label for="smtp-enabled">Enabled <SettingBadges path="smtp.enabled" /></label>
        </div>
        <SettingNote path="smtp.enabled" />
        <br v-if="enabled" />
        <label class="field-label" for="smtp-from" v-if="enabled">From Address <SettingBadges path="smtp.from" /></label>
        <ShadInput id="smtp-from" v-setting="settingLocked('smtp.from')" type="text" v-model="from" v-if="enabled" :aria-invalid="(errors && errors.from) || false" />
        <p v-if="enabled">The email address used as the From address in email notifications</p>
        <SettingNote v-if="enabled" path="smtp.from" />
        <span v-if="enabled && errors && errors.from" class="p-error">{{ errors.from }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="smtp-auth-method" v-if="enabled">Authentication Method <SettingBadges path="smtp.auth-method" /></label>
        <select id="smtp-auth-method" v-setting="settingLocked('smtp.auth-method')" v-model="authMethod" v-if="enabled" class="ui-select" :class="{ 'ui-select-invalid': (errors && errors['auth-method']) || false }">
          <option v-for="option in [
            { label: 'Plain', value: 'plain' },
            { label: 'Login', value: 'login' },
            { label: 'None', value: 'none' },
          ]" :key="option.value" :value="option.value">{{ option.label }}</option>
        </select>
        <p v-if="enabled">
          The authentication method to use when connecting to the SMTP server.
          One of <code>plain</code>, <code>login</code>, or <code>none</code>.
        </p>
        <SettingNote v-if="enabled" path="smtp.auth-method" />
        <span v-if="enabled && errors && errors['auth-method']" class="p-error">{{ errors['auth-method'] }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="smtp-host" v-if="enabled">Host <SettingBadges path="smtp.host" /></label>
        <ShadInput id="smtp-host" v-setting="settingLocked('smtp.host')" type="text" v-model="host" v-if="enabled" :aria-invalid="(errors && errors.host) || false" />
        <p v-if="enabled">
          The hostname or IP address of the SMTP server to connect to.
        </p>
        <SettingNote v-if="enabled" path="smtp.host" />
        <span v-if="enabled && errors && errors.host" class="p-error">{{ errors.host }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="smtp-port" v-if="enabled">Port <SettingBadges path="smtp.port" /></label>
        <ShadInput id="smtp-port" v-setting="settingLocked('smtp.port')" type="number" v-model="port" v-if="enabled"
          :aria-invalid="(errors && errors.port) || false" />
        <p v-if="enabled">
          The port number of the SMTP server to connect to.
        </p>
        <SettingNote v-if="enabled" path="smtp.port" />
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="smtp-username" v-if="enabled">Username <SettingBadges path="smtp.username" /></label>
        <ShadInput id="smtp-username" v-setting="settingLocked('smtp.username')" type="text" v-model="username" v-if="enabled"
          :aria-invalid="(errors && errors.username) || false" />
        <p v-if="enabled">
          The username to use when connecting to the SMTP server.
        </p>
        <SettingNote v-if="enabled" path="smtp.username" />
        <span v-if="enabled && errors && errors.username" class="p-error">{{ errors.username }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="smtp-password" v-if="enabled">Password <SettingBadges path="smtp.password" /></label>
        <ShadInput id="smtp-password" v-setting="settingLocked('smtp.password')" type="password" v-model="password" v-if="enabled"
          :aria-invalid="(errors && errors.password) || false" />
        <p v-if="enabled">
          The password to use when connecting to the SMTP server.
        </p>
        <SettingNote v-if="enabled" path="smtp.password" />
        <small v-if="enabled" class="p-text-secondary">{{ passwordStatusMessage }}</small>
        <span v-if="enabled && errors && errors.password" class="p-error">{{ errors.password }}</span>
        <br v-if="enabled" />
        <br v-if="enabled" />
        <label class="field-label" for="smtp-tls" v-if="enabled">TLS Mode <SettingBadges path="smtp.tls" /></label>
        <select id="smtp-tls" v-setting="settingLocked('smtp.tls')" v-model="tls" v-if="enabled" class="ui-select" :class="{ 'ui-select-invalid': (errors && errors.tls) || false }">
          <option v-for="option in [
            { label: 'None', value: 'none' },
            { label: 'STARTTLS', value: 'starttls' },
            { label: 'Implicit', value: 'implicit' },
          ]" :key="option.value" :value="option.value">{{ option.label }}</option>
        </select>
        <p v-if="enabled">
          The TLS mode to use when connecting to the SMTP server. One of
          <code>none</code>, <code>starttls</code>, or <code>implicit</code>.
        </p>
        <SettingNote v-if="enabled" path="smtp.tls" />
        <span v-if="enabled && errors && errors.tls" class="p-error">{{ errors.tls }}</span>
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
import { settingMixin } from '@/lib/settingMixin';

export default {
  mixins: [settingMixin],
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
      set(value: boolean) {
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
      set(value: string) {
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
      set(value: string) {
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
          'port': Number(value) || 0,
        });
      },
    },
    authMethod: {
      get() {
        return (this.modelValue && this.modelValue['auth-method']) || '';
      },
      set(value: string) {
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
