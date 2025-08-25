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
      <template #title>Metrics Settings</template>
      <template #content>
        <p>DMRHub can export metrics in the Prometheus format and traces in the OTLP format.
          These settings enable a separate HTTP server that serves the metrics endpoints.
        </p>
        <br />
        <span class="p-float-label">
          <InputText
            id="otlpEndpoint"
            type="text"
            v-model="otlpEndpoint"
            :class="{ 'p-invalid': (errors && errors['otlp-endpoint']) || false }"
          />
          <label for="otlpEndpoint">OTLP Endpoint</label>
        </span>
        <p>
          The OTLP endpoint to send traces to. This can be a URL or an address in the format.
          If empty, tracing is disabled.
        </p>
        <span v-if="errors && errors['otlp-endpoint']" class="p-error">{{ errors['otlp-endpoint'] }}</span>
        <br />
        <span>
          <Checkbox
            id="enabled"
            inputId="enabled"
            v-model="enabled"
            :binary="true"
          />
          <label for="enabled">&nbsp;&nbsp;Enable Prometheus Metrics</label>
        </span>
        <br v-if="enabled" />
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText id="bind" type="text" v-model="bind" :class="{ 'p-invalid': (errors && errors.bind) || false }" />
          <label for="bind">Bind</label>
        </span>
        <p v-if="enabled">
          The address to bind the metrics server to
        </p>
        <span v-if="enabled && errors && errors.bind" class="p-error">{{ errors.bind }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <InputText
            id="port"
            type="number"
            v-model="port"
            :class="{ 'p-invalid': (errors && errors.port) || false }"
          />
          <label for="port">Port</label>
        </span>
        <p v-if="enabled">
          The port number to bind the metrics server to
        </p>
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <span class="p-float-label" v-if="enabled">
          <TextArea
            rows="5"
            id="trustedProxies"
            v-model="trustedProxies"
            :class="{ 'p-invalid': (errors && errors['trusted-proxies']) || false }"
          />
          <label for="trustedProxies">Trusted Proxies</label>
        </span>
        <p v-if="enabled">
          A list of trusted proxy IP addresses. If set, the metrics server will only accept
          requests from these IP addresses. One per line.
        </p>
        <span v-if="enabled && errors && errors['trusted-proxies']" class="p-error">
          {{ errors['trusted-proxies'] }}
        </span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import Checkbox from 'primevue/checkbox';
import InputText from 'primevue/inputtext';
import TextArea from 'primevue/textarea';

export default {
  components: {
    Card,
    Checkbox,
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
    },
  },
  emits: ['update:modelValue'],
  computed: {
    enabled: {
      get() {
        return (this.modelValue && this.modelValue.enabled) || false;
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'enabled': value,
        });
      },
    },
    otlpEndpoint: {
      get() {
        return (this.modelValue && this.modelValue['otlp-endpoint']) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'otlp-endpoint': value,
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
  },
  data: function() {
    return {};
  },
  mounted() {},
};
</script>
