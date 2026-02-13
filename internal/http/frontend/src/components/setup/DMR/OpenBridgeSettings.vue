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
        <CardTitle>OpenBridge Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>DMRHub can run an OpenBridge server to allow connections from DMR radios and software.
          This is useful for connecting to other DMR networks.</p>
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
        <label class="field-label" for="bind" v-if="enabled">Bind</label>
        <ShadInput id="bind" type="text" v-model="bind" v-if="enabled" :aria-invalid="(errors && errors.bind) || false" />
        <p v-if="enabled">
          The address to bind the OpenBridge server to
        </p>
        <span v-if="errors && errors.bind" class="p-error">{{ errors.bind }}</span>
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
          The port number to bind the OpenBridge server to
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
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
    },
  },
  emits: ['update:modelValue'],
  computed: {
    enabled: {
      get() {
        return (this.modelValue && this.modelValue['enabled']) || false;
      },
      set(value: boolean) {
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
      set(value: string) {
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
      set(value: string) {
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
