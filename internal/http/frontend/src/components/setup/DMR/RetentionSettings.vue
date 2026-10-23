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
  <div data-test="retention-card">
    <Card>
      <CardHeader>
        <CardTitle>Retention</CardTitle>
      </CardHeader>
      <CardContent>
        <p>How long DMRHub keeps records.</p>
        <template v-for="field in fields" :key="field.key">
          <br />
          <label class="field-label" :for="`retention-${field.key}`">{{ field.label }}</label>
          <ShadSelect
            :model-value="String(value(field.key))"
            @update:model-value="(v) => set(field.key, v)"
          >
            <SelectTrigger :id="`retention-${field.key}`" class="w-full max-w-80" :data-test="`retention-${field.key}`">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="opt in options(field.key)" :key="opt.value" :value="String(opt.value)">
                {{ opt.label }}
              </SelectItem>
            </SelectContent>
          </ShadSelect>
          <p>{{ field.help }}</p>
          <span v-if="errors && errors[field.key]" class="p-error">{{ errors[field.key] }}</span>
        </template>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select as ShadSelect,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { retentionChoices, retentionDays } from '@/lib/retention';

type RetentionKey = 'messages' | 'transmissions' | 'bursts';

const props = defineProps<{
  modelValue: Record<string, unknown>;
  errors?: Record<string, string>;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', value: Record<string, unknown>): void
}>();

const fields: { key: RetentionKey; label: string; help: string }[] = [
  { key: 'messages', label: 'Messages', help: 'Text and data messages, with their raw bytes.' },
  { key: 'transmissions', label: 'Transmissions', help: 'Last heard entries for voice, data and control traffic.' },
  { key: 'bursts', label: 'Raw bursts', help: 'The raw DMR bursts of each transmission.' },
];

const value = (key: RetentionKey): number => retentionDays(props.modelValue?.[key]);

const options = (key: RetentionKey) => retentionChoices(value(key));

const set = (key: RetentionKey, v: unknown) => {
  const days = Number(v);
  if (!Number.isFinite(days)) {
    return;
  }
  emit('update:modelValue', { ...props.modelValue, [key]: days });
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}
</style>
