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
        <CardTitle>PProf Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>PProf is a tool for analyzing the performance of Go applications. This setting enables a separate
          HTTP server that serves the pprof endpoints. It is recommended to only enable this in development or
          debugging scenarios as it can expose sensitive information about the application.
        </p>
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
          The address to bind the pprof server to
        </p>
        <span v-if="enabled && errors && errors.bind" class="p-error">{{ errors.bind }}</span>
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
          The port number to bind the pprof server to
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="trustedProxies" v-if="enabled">Trusted Proxies</label>
        <textarea
          rows="5"
          id="trustedProxies"
          v-model="trustedProxies"
          v-if="enabled"
          class="ui-textarea"
          :class="{ 'ui-textarea-invalid': (errors && errors['trusted-proxies']) || false }"
        />
        <p v-if="enabled">
          A list of trusted proxy IP addresses. If set, the pprof server will only accept
          requests from these IP addresses. One per line.
        </p>
        <span v-if="enabled && errors && errors['trusted-proxies']" class="p-error">
          {{ errors['trusted-proxies'] }}
        </span>
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
    trustedProxies: {
      get() {
        const trustedProxies = this.modelValue && this.modelValue['trusted-proxies'];
        return Array.isArray(trustedProxies) ? trustedProxies.join('\n') : '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'trusted-proxies': value.split('\n'),
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

.ui-textarea {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}

.ui-textarea-invalid {
  border-color: var(--primary);
}
</style>
