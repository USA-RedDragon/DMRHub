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
          <InputText id="bind" type="text" v-model="bind" />
          <label for="bind">Bind</label>
        </span>
        <p>
          The address to bind the HTTP server to
        </p>
        <br />
        <span class="p-float-label">
          <InputText id="port" type="number" v-model="port" />
          <label for="port">Port</label>
        </span>
        <p>
          The port number to bind the HTTP server to
        </p>
        <br />
        <span class="p-float-label">
          <TextArea rows="5" id="trustedProxies" v-model="trustedProxies" />
          <label for="trustedProxies">Trusted Proxies</label>
        </span>
        <p>
          A list of trusted proxy IP addresses. If set, the HTTP server will only accept
          requests from these IP addresses. One per line.
        </p>
        <br />
        <span class="p-float-label">
          <InputText id="canonicalHost" type="text" v-model="canonicalHost" />
          <label for="canonicalHost">Canonical Host</label>
        </span>
        <p>
          The canonical host name for the DMRHub instance. This is used to generate absolute URLs.
        </p>
        <br />
        <RobotsTXTSettings v-model="robotsTXT" />
        <br />
        <CORSSettings v-model="cors" />
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
