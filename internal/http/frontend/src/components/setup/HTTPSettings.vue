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
      <template #title>HTTP Settings</template>
      <template #content>
        <p>These settings control the main HTTP server that serves the web interface and API.
          A canonical host is required by DMRHub to allow it to generate absolute URLs.</p>
        <br />
        <span class="p-float-label">
          <InputText id="bind" type="text" v-model="bind" :class="{ 'p-invalid': (errors && errors.bind) || false }" />
          <label for="bind">Bind</label>
        </span>
        <p>
          The address to bind the HTTP server to
        </p>
        <span v-if="errors && errors.bind" class="p-error">{{ errors.bind }}</span>
        <br />
        <span class="p-float-label">
          <InputText
            id="port"
            type="number"
            v-model="port"
            :class="{ 'p-invalid': (errors && errors.port) || false }"
          />
          <label for="port">Port</label>
        </span>
        <p>
          The port number to bind the HTTP server to
        </p>
        <span v-if="errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br />
        <span class="p-float-label">
          <TextArea
            rows="5"
            id="trustedProxies"
            v-model="trustedProxies"
            :class="{ 'p-invalid': (errors && errors['trusted-proxies']) || false }"
          />
          <label for="trustedProxies">Trusted Proxies</label>
        </span>
        <p>
          A list of trusted proxy IP addresses. If set, the HTTP server will only accept
          requests from these IP addresses. One per line.
        </p>
        <span v-if="errors && errors['trusted-proxies']" class="p-error">{{ errors['trusted-proxies'] }}</span>
        <br />
        <span class="p-float-label">
          <InputText
            id="canonicalHost"
            type="text"
            v-model="canonicalHost"
            :class="{ 'p-invalid': (errors && errors['canonical-host']) || false }"
          />
          <label for="canonicalHost">Canonical Host</label>
        </span>
        <p>
          The canonical host name for the DMRHub instance. This is used to generate absolute URLs.
        </p>
        <span v-if="errors && errors['canonical-host']" class="p-error">{{ errors['canonical-host'] }}</span>
        <br />
        <RobotsTXTSettings v-model="robotsTXT" :errors="errors['robots-txt']" />
        <br />
        <CORSSettings v-model="cors" :errors="errors.cors" />
      </template>
    </Card>
  </div>
</template>

<script>
import RobotsTXTSettings from './HTTP/RobotsTXTSettings.vue';
import CORSSettings from './HTTP/CORSSettings.vue';

import Card from 'primevue/card';
import InputText from 'primevue/inputtext';
import TextArea from 'primevue/textarea';

export default {
  components: {
    RobotsTXTSettings,
    CORSSettings,
    Card,
    InputText,
    TextArea,
  },
  props: {
    modelValue: {
      type: Object,
      required: true,
    },
    errors: {
      type: Object,
      required: true,
      default: () => ({}),
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
    robotsTXT: {
      get() {
        return (this.modelValue && this.modelValue['robots-txt']) || {};
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'robots-txt': value,
        });
      },
    },
    cors: {
      get() {
        return (this.modelValue && this.modelValue['cors']) || {};
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'cors': value,
        });
      },
    },
    trustedProxies: {
      get() {
        return (
          this.modelValue &&
          this.modelValue['trusted-proxies'] &&
          this.modelValue['trusted-proxies'].join('\n')) || [];
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'trusted-proxies': value.split('\n'),
        });
      },
    },
    canonicalHost: {
      get() {
        return (this.modelValue && this.modelValue['canonical-host']) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'canonical-host': value,
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
