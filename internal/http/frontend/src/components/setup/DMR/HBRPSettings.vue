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
      <template #title>Homebrew Repeater Protocol Settings</template>
      <template #content>
        <p>DMRHub can run an HBRP server to allow connections from DMR radios and
          software such as DMRGateway.</p>
        <br />
        <span class="p-float-label">
          <InputText id="bind" type="text" v-model="bind" />
          <label for="bind">Bind</label>
        </span>
        <p>
          The address to bind the HBRP server to
        </p>
        <br />
        <span class="p-float-label">
          <InputText id="port" type="number" v-model="port" />
          <label for="port">Port</label>
        </span>
        <p>
          The port number to bind the HBRP server to
        </p>
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
