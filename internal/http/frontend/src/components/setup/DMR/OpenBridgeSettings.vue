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
      <template #title>OpenBridge Settings</template>
      <template #content>
        <p>DMRHub can run an OpenBridge server to allow connections from DMR radios and software.
          This is useful for connecting to other DMR networks.</p>
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
          <InputText id="bind" type="text" v-model="bind" :class="{ 'p-invalid': (errors && errors.bind) || false }" />
          <label for="bind">Bind</label>
        </span>
        <p v-if="enabled">
          The address to bind the OpenBridge server to
        </p>
        <span v-if="errors && errors.bind" class="p-error">{{ errors.bind }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="port" type="number" v-model="port" :class="{ 'p-invalid': (errors && errors.port) || false }" />
          <label for="port">Port</label>
        </span>
        <p v-if="enabled">
          The port number to bind the OpenBridge server to
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import InputText from 'primevue/inputtext';
import Checkbox from 'primevue/checkbox';

export default {
  components: {
    Card,
    InputText,
    Checkbox,
  },
  props: {
    modelValue: {
      type: Object,
      required: true,
    },
    errors: {
      type: Object,
    },
  },
  emits: ['update:modelValue'],
  computed: {
    enabled: {
      get() {
        return (this.modelValue && this.modelValue['enabled']) || false;
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'enabled': value,
        });
      },
    },
    bind: {
      get() {
        return (this.modelValue && this.modelValue['bind']) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'bind': value,
        });
      },
    },
    port: {
      get() {
        return (this.modelValue && this.modelValue['port']) || undefined;
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'port': value,
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
