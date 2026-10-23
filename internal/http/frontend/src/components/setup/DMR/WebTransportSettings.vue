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
  <Card>
    <CardHeader>
      <CardTitle>WebTransport</CardTitle>
    </CardHeader>
    <CardContent>
      <p>Hotspots and radios that run in a web browser connect to the MMDVM server over WebTransport. They log in
        with the repeater ID and password, like any other hotspot.</p>
      <br />
      <div class="flex items-center gap-3">
        <ShadSwitch
          id="webtransport-enabled"
          aria-labelledby="webtransport-enabled-label"
          :model-value="enabled"
          @update:model-value="setEnabled"
        />
        <label id="webtransport-enabled-label" for="webtransport-enabled">Accept browser hotspots and radios</label>
      </div>
      <div v-if="enabled" data-testid="webtransport-fields">
        <br />
        <label class="field-label" for="webtransport-port">Port</label>
        <ShadInput
          id="webtransport-port"
          type="number"
          :model-value="port"
          :aria-invalid="(errors && errors.port) || false"
          @update:model-value="(v: string | number) => update('port', Number(v) || 0)"
        />
        <p>The UDP port that browsers connect to. It must differ from the MMDVM port.</p>
        <span v-if="errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br />
        <label class="field-label" for="webtransport-cert-file">Certificate file</label>
        <ShadInput
          id="webtransport-cert-file"
          type="text"
          placeholder="/tls/tls.crt"
          :model-value="certFile"
          @update:model-value="(v: string | number) => update('cert-file', String(v))"
        />
        <p>The TLS certificate, such as the one that cert-manager mounts. DMRHub reads it again when it changes.</p>
        <br />
        <label class="field-label" for="webtransport-key-file">Key file</label>
        <ShadInput
          id="webtransport-key-file"
          type="text"
          placeholder="/tls/tls.key"
          :model-value="keyFile"
          :aria-invalid="(errors && errors['key-file']) || false"
          @update:model-value="(v: string | number) => update('key-file', String(v))"
        />
        <p>The key of the certificate.</p>
        <span v-if="errors && errors['key-file']" class="p-error">{{ errors['key-file'] }}</span>
        <p v-if="!certFile && !keyFile" class="text-muted-foreground" data-testid="webtransport-own">
          With no certificate file, DMRHub uses certificates of its own, which browsers accept with no setup.
        </p>
      </div>
    </CardContent>
  </Card>
</template>

<script lang="ts">
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch as ShadSwitch } from '@/components/ui/switch';

const defaultPort = 62032;

export default {
  components: {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    ShadInput,
    ShadSwitch,
  },
  props: {
    modelValue: {
      type: Object,
      default: () => ({}),
    },
    errors: {
      type: Object,
    },
  },
  emits: ['update:modelValue'],
  computed: {
    enabled(): boolean {
      return Boolean(this.modelValue && this.modelValue['enabled']);
    },
    port(): number {
      return (this.modelValue && this.modelValue['port']) || defaultPort;
    },
    certFile(): string {
      return (this.modelValue && this.modelValue['cert-file']) || '';
    },
    keyFile(): string {
      return (this.modelValue && this.modelValue['key-file']) || '';
    },
  },
  methods: {
    update(key: string, value: unknown) {
      this.$emit('update:modelValue', { ...this.modelValue, [key]: value });
    },
    setEnabled(value: boolean) {
      this.$emit('update:modelValue', { ...this.modelValue, port: this.port, enabled: value });
    },
  },
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}
</style>
