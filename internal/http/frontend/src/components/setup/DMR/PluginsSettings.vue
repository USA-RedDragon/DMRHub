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
  <div data-test="plugins-card">
    <Card>
      <CardHeader>
        <CardTitle>Plugins</CardTitle>
      </CardHeader>
      <CardContent>
        <p>Plugins answer the calls to their IDs. Give them their IDs on the Plugins page under Admin.</p>
        <div class="mt-4 flex items-center gap-3">
          <Switch
            id="plugins-in-process"
            aria-labelledby="plugins-in-process-label"
            :model-value="inProcess"
            @update:model-value="(v: boolean) => set('in-process', v)"
          />
          <label id="plugins-in-process-label" for="plugins-in-process" class="text-sm">Run plugins inside DMRHub</label>
        </div>
        <p v-if="inProcess" class="mt-2 text-sm text-muted-foreground" data-test="in-process-on">
          The parrot, the location server and the plugins you upload run as part of DMRHub, with nothing else to install.
        </p>
        <p v-else class="mt-2 text-sm text-muted-foreground" data-test="in-process-off">
          Turn this off only if you run every plugin as its own program, such as dmrhub-parrot and dmrhub-locator.
        </p>
        <div class="mt-4 flex items-center gap-3">
          <Switch id="plugins-enabled" aria-labelledby="plugins-enabled-label" :model-value="enabled" @update:model-value="(v: boolean) => set('enabled', v)" />
          <label id="plugins-enabled-label" for="plugins-enabled" class="text-sm">Accept plugins that run as their own programs</label>
        </div>
        <div v-if="enabled" class="mt-4 grid gap-4 sm:grid-cols-3" data-test="plugins-fields">
          <div>
            <label class="field-label" for="plugins-port">Port</label>
            <ShadInput
              id="plugins-port"
              inputmode="numeric"
              :model-value="String(port)"
              :aria-invalid="errors?.port ? true : undefined"
              @update:model-value="(v) => set('port', Number(v) || 0)"
            />
            <p class="text-sm text-muted-foreground">The TCP port that plugins connect to.</p>
            <span v-if="errors?.port" class="p-error">{{ errors.port }}</span>
          </div>
          <div>
            <label class="field-label" for="plugins-cert-file">Certificate file</label>
            <ShadInput
              id="plugins-cert-file"
              placeholder="/tls/tls.crt"
              :model-value="certFile"
              @update:model-value="(v) => set('cert-file', String(v))"
            />
            <p class="text-sm text-muted-foreground">The TLS certificate. DMRHub reads it again when it changes.</p>
          </div>
          <div>
            <label class="field-label" for="plugins-key-file">Key file</label>
            <ShadInput
              id="plugins-key-file"
              placeholder="/tls/tls.key"
              :model-value="keyFile"
              :aria-invalid="errors?.['key-file'] ? true : undefined"
              @update:model-value="(v) => set('key-file', String(v))"
            />
            <p class="text-sm text-muted-foreground">The key of the certificate.</p>
            <span v-if="errors?.['key-file']" class="p-error">{{ errors['key-file'] }}</span>
          </div>
        </div>
        <p v-if="enabled && !certFile && !keyFile" class="mt-3 text-sm text-muted-foreground" data-test="plugins-own-cert">
          With no certificate file, DMRHub uses a certificate of its own. Plugins then check it by the fingerprint on the
          Plugins page.
        </p>
        <p v-if="enabled" class="mt-3 text-sm text-muted-foreground">DMRHub starts listening for plugins when it restarts.</p>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';

const DEFAULT_PORT = 3006;

const props = defineProps<{
  modelValue: Record<string, unknown>;
  errors?: Record<string, string>;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', value: Record<string, unknown>): void
}>();

const enabled = computed(() => props.modelValue?.enabled === true);
const inProcess = computed(() => props.modelValue?.['in-process'] !== false);
const port = computed(() => (typeof props.modelValue?.port === 'number' && props.modelValue.port > 0 ? props.modelValue.port : DEFAULT_PORT));
const certFile = computed(() => String(props.modelValue?.['cert-file'] ?? ''));
const keyFile = computed(() => String(props.modelValue?.['key-file'] ?? ''));

const set = (key: string, v: unknown) => {
  emit('update:modelValue', { ...props.modelValue, [key]: v });
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
  font-weight: 500;
}
</style>
