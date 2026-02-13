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
      <CardHeader>
        <CardTitle>HTTP Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>These settings control the main HTTP server that serves the web interface and API.
          A canonical host is required by DMRHub to allow it to generate absolute URLs.</p>
        <br />
        <label class="field-label" for="bind">Bind</label>
        <ShadInput id="bind" type="text" v-model="bind" :aria-invalid="(errors && errors.bind) || false" />
        <p>
          The address to bind the HTTP server to
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
          The port number to bind the HTTP server to
        </p>
        <span v-if="errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br />
        <label class="field-label" for="trustedProxies">Trusted Proxies</label>
        <textarea
          rows="5"
          id="trustedProxies"
          v-model="trustedProxies"
          class="ui-textarea"
          :class="{ 'ui-textarea-invalid': (errors && errors['trusted-proxies']) || false }"
        />
        <p>
          A list of trusted proxy IP addresses. If set, the HTTP server will only accept
          requests from these IP addresses. One per line.
        </p>
        <span v-if="errors && errors['trusted-proxies']" class="p-error">{{ errors['trusted-proxies'] }}</span>
        <br />
        <label class="field-label" for="canonicalHost">Canonical Host</label>
        <ShadInput
          id="canonicalHost"
          type="text"
          v-model="canonicalHost"
          :aria-invalid="(errors && errors['canonical-host']) || false"
        />
        <p>
          The canonical host name for the DMRHub instance. This is used to generate absolute URLs.
        </p>
        <span v-if="errors && errors['canonical-host']" class="p-error">{{ errors['canonical-host'] }}</span>
        <br />
        <RobotsTXTSettings v-model="robotsTXT" :errors="errors['robots-txt']" />
        <br />
        <CORSSettings v-model="cors" :errors="errors.cors" />
      </CardContent>
    </Card>
  </div>
</template>

<script lang="ts">
import RobotsTXTSettings from './HTTP/RobotsTXTSettings.vue';
import CORSSettings from './HTTP/CORSSettings.vue';

import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';

export default {
  components: {
    RobotsTXTSettings,
    CORSSettings,
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
      default: () => ({}),
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
    robotsTXT: {
      get() {
        return (this.modelValue && this.modelValue['robots-txt']) || {};
      },
      set(value: Record<string, unknown>) {
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
      set(value: Record<string, unknown>) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'cors': value,
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
    canonicalHost: {
      get() {
        return (this.modelValue && this.modelValue['canonical-host']) || '';
      },
      set(value: string) {
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

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
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
