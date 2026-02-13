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
        <CardTitle>IPSC Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>DMRHub can run an IPSC server to allow connections from Motorola DMR radios</p>
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
        <label class="field-label" for="bind" v-if="enabled">Bind Address</label>
        <ShadInput id="bind" type="text" v-model="bind" v-if="enabled" :aria-invalid="(errors && errors.bind) || false" />
        <p v-if="enabled">
          The address to bind the IPSC server to
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
          The port number to bind the IPSC server to
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="network-id" v-if="enabled">Network ID</label>
        <ShadInput
          id="network-id"
          type="number"
          v-model="networkId"
          v-if="enabled"
          :aria-invalid="(errors && errors['network-id']) || false"
        />
        <p v-if="enabled">
          DMR peer ID for this IPSC master server (e.g., your repeater's radio ID)
        </p>
        <span v-if="enabled && errors && errors['network-id']" class="p-error">{{ errors['network-id'] }}</span>
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
    networkId: {
      get() {
        return (this.modelValue && this.modelValue['network-id']) || undefined;
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'network-id': value,
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
