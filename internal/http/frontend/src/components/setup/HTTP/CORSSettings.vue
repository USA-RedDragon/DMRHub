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
        <CardTitle>CORS Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>CORS (Cross-Origin Resource Sharing) settings control which external domains
          are allowed to access the DMRHub API. This is important for web applications
          that run in the browser and need to make requests to the DMRHub server.
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
        <label class="field-label" for="hosts" v-if="enabled">Hosts</label>
        <textarea rows="5" id="hosts" v-model="hosts" v-if="enabled" class="ui-textarea" :class="{ 'ui-textarea-invalid': (errors && errors.hosts) || false }" />
        <p v-if="enabled">
          A list of hosts that are allowed to access the DMRHub API.
          Use <code>*</code> to allow all hosts (not recommended for production). One per line.
        </p>
        <span v-if="enabled && errors && errors.hosts" class="p-error">{{ errors.hosts }}</span>
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

export default {
  components: {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
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
    hosts: {
      get() {
        const extraHosts = this.modelValue && this.modelValue['extra-hosts'];
        return Array.isArray(extraHosts) ? extraHosts.join('\n') : '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'extra-hosts': value.split('\n'),
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
  border: 1px solid hsl(var(--border));
  border-radius: 0.5rem;
  background: hsl(var(--background));
  color: hsl(var(--foreground));
  padding: 0.5rem 0.75rem;
}

.ui-textarea-invalid {
  border-color: hsl(var(--primary));
}
</style>
