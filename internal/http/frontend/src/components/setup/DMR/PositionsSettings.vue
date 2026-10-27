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
  <div data-test="positions-card">
    <Card>
      <CardHeader>
        <CardTitle>Positions</CardTitle>
      </CardHeader>
      <CardContent>
        <p>Limits for the radios whose owners turn on position tracking.</p>
        <div class="grid gap-4 sm:grid-cols-2 mt-4">
          <div>
            <label class="field-label" for="positions-min-interval">Shortest report interval <SettingBadges path="dmr.positions.min-interval" /></label>
            <div class="flex items-center gap-2">
              <ShadInput
                id="positions-min-interval" v-setting="settingLocked('dmr.positions.min-interval')"
                class="w-24"
                inputmode="numeric"
                :model-value="String(interval)"
                :aria-invalid="(errors && errors['min-interval']) ? true : undefined"
                @update:model-value="(v) => setNumber('min-interval', v)"
              />
              <SettingNote path="dmr.positions.min-interval" />
              <span class="text-sm text-muted-foreground">minutes</span>
            </div>
            <p class="text-sm text-muted-foreground">Owners can pick 5, 10, 15, 30 or 60 minutes, but not less than this.</p>
            <span v-if="errors && errors['min-interval']" class="p-error">{{ errors['min-interval'] }}</span>
          </div>
          <div>
            <label class="field-label" for="positions-max-retention">Longest time to keep positions <SettingBadges path="dmr.positions.max-retention" /></label>
            <div class="flex items-center gap-2">
              <ShadInput
                id="positions-max-retention" v-setting="settingLocked('dmr.positions.max-retention')"
                class="w-24"
                inputmode="numeric"
                :model-value="String(retention)"
                :aria-invalid="(errors && errors['max-retention']) ? true : undefined"
                @update:model-value="(v) => setNumber('max-retention', v)"
              />
              <SettingNote path="dmr.positions.max-retention" />
              <span class="text-sm text-muted-foreground">days, up to 365</span>
            </div>
            <p class="text-sm text-muted-foreground">Owners can keep a radio's positions for 24 hours, 7 days, 30 days or this.</p>
            <span v-if="errors && errors['max-retention']" class="p-error">{{ errors['max-retention'] }}</span>
          </div>
        </div>
        <div class="border-t mt-6 pt-6">
          <h3 class="font-semibold">APRS-IS</h3>
          <p class="text-sm text-muted-foreground">
            The hub logs in to APRS-IS once and sends the positions of the radios whose owners turn it on, each under
            its owner's callsign.
          </p>
          <div class="grid gap-4 sm:grid-cols-2 mt-4">
            <div>
              <label class="field-label" for="positions-aprs-callsign">Callsign <SettingBadges path="dmr.positions.aprs-is.callsign" /></label>
              <ShadInput
                id="positions-aprs-callsign" v-setting="settingLocked('dmr.positions.aprs-is.callsign')"
                :model-value="aprs.callsign || ''"
                :aria-invalid="aprsErrors.callsign ? true : undefined"
                @update:model-value="(v) => setAPRS('callsign', String(v).toUpperCase())"
              />
              <SettingNote path="dmr.positions.aprs-is.callsign" />
              <span v-if="aprsErrors.callsign" class="p-error">{{ aprsErrors.callsign }}</span>
            </div>
            <div>
              <label class="field-label" for="positions-aprs-server">Server <SettingBadges path="dmr.positions.aprs-is.server" /></label>
              <ShadInput
                id="positions-aprs-server" v-setting="settingLocked('dmr.positions.aprs-is.server')"
                :model-value="aprs.server || ''"
                placeholder="rotate.aprs2.net:14580"
                :aria-invalid="aprsErrors.server ? true : undefined"
                @update:model-value="(v) => setAPRS('server', String(v))"
              />
              <SettingNote path="dmr.positions.aprs-is.server" />
              <span v-if="aprsErrors.server" class="p-error">{{ aprsErrors.server }}</span>
            </div>
          </div>
          <p class="text-sm text-muted-foreground mt-2">
            The hub works out the APRS-IS passcode from the callsign. Leave the callsign empty to send nothing to
            APRS-IS.
          </p>
        </div>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, inject } from 'vue';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import SettingBadges from '@/components/setup/SettingBadges.vue';
import SettingNote from '@/components/setup/SettingNote.vue';
import { setupStateKey, settingLocked as lockedIn } from '@/lib/setupState';
import { settingDirective as vSetting } from '@/lib/settingDirective';

const DEFAULT_INTERVAL = 5;
const DEFAULT_RETENTION = 30;

const setupState = inject(setupStateKey, null);
const settingLocked = (path: string) => lockedIn(setupState, path);

const props = defineProps<{
  modelValue: Record<string, unknown>;
  errors?: Record<string, unknown>;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', value: Record<string, unknown>): void
}>();

const positive = (v: unknown, fallback: number): number | string => {
  if (typeof v === 'string') {
    return v;
  }
  return typeof v === 'number' && v > 0 ? v : fallback;
};

const interval = computed(() => positive(props.modelValue?.['min-interval'], DEFAULT_INTERVAL));
const retention = computed(() => positive(props.modelValue?.['max-retention'], DEFAULT_RETENTION));
const aprs = computed(() => (props.modelValue?.['aprs-is'] as Record<string, string> | undefined) || {});

const aprsErrors = computed(() => (props.errors?.['aprs-is'] as Record<string, string> | undefined) || {});

const setNumber = (key: string, v: unknown) => {
  const text = String(v).trim();
  const n = Number(text);
  emit('update:modelValue', { ...props.modelValue, [key]: text !== '' && Number.isInteger(n) ? n : text });
};

const setAPRS = (key: string, v: string) => {
  emit('update:modelValue', { ...props.modelValue, 'aprs-is': { ...aprs.value, [key]: v } });
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
  font-weight: 500;
}
</style>
