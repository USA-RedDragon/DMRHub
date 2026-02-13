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
        <CardTitle>MMDVM Protocol Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>DMRHub can run an MMDVM server to allow connections from DMR radios and
          software such as DMRGateway.</p>
        <br />
        <label class="field-label" for="bind">Bind</label>
        <ShadInput id="bind" type="text" v-model="bind" :aria-invalid="(errors && errors.bind) || false" />
        <p>
          The address to bind the MMDVM server to
        </p>
        <span v-if="errors && errors.bind" class="p-error">{{ errors.bind }}</span>
        <br />
        <label class="field-label" for="port">Port</label>
        <ShadInput
          id="port"
          type="number"
          v-model="port"
          :aria-invalid="(errors && errors.port) || false"
        />
        <p>
          The port number to bind the MMDVM server to
        </p>
        <span v-if="errors && errors.port" class="p-error">{{ errors.port }}</span>
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
</style>
