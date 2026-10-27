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
  <div class="mb-5" :data-test="`field-${field.path}`" :data-locked="locked ? 'true' : undefined">
    <div v-if="field.kind === 'bool'" class="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 font-semibold">
      <Switch
        :id="id"
        :model-value="normalized === true"
        :disabled="disabled"
        @update:model-value="(v: boolean) => $emit('set', field.path, v)"
      />
      <label :for="id" :class="disabled ? 'cursor-not-allowed' : 'cursor-pointer'">{{ field.label }}</label>
      <SourceChips :setting="setting" />
    </div>
    <template v-else>
      <div class="mb-1.5 flex flex-wrap items-center gap-x-2 gap-y-1.5 font-semibold">
        <label v-if="labelFor" :for="id">{{ field.label }}</label>
        <span v-else :id="`${id}-label`">{{ field.label }}</span>
        <SourceChips :setting="setting" />
      </div>
      <select
        v-if="field.kind === 'select'"
        :id="id"
        :class="[boxClass, 'h-9 bg-background']"
        :disabled="disabled"
        :value="String(normalized)"
        :aria-invalid="error ? true : undefined"
        @change="(e) => set((e.target as HTMLSelectElement).value)"
      >
        <option v-for="o in field.options" :key="String(o.value)" :value="String(o.value)">{{ o.label }}</option>
      </select>
      <div
        v-else-if="field.kind === 'choice'"
        role="radiogroup"
        :aria-labelledby="`${id}-label`"
        class="grid gap-2"
      >
        <label
          v-for="o in field.options"
          :key="String(o.value)"
          class="flex items-start gap-2.5 rounded-lg border px-3 py-2.5"
          :class="[
            normalized === o.value ? 'border-primary shadow-[inset_0_0_0_1px_var(--primary)]' : 'border-border',
            disabled ? 'cursor-not-allowed opacity-70' : 'cursor-pointer',
          ]"
        >
          <input
            type="radio"
            class="mt-1 accent-primary"
            :name="id"
            :value="String(o.value)"
            :checked="normalized === o.value"
            :disabled="disabled"
            @change="set(String(o.value))"
          />
          <span>
            <b class="block">{{ o.label }}<span v-if="isDefault(o.value)" class="ml-1.5 text-[11px] font-bold text-primary">DEFAULT</span></b>
            <span v-if="o.description" class="text-sm text-muted-foreground">{{ o.description }}</span>
          </span>
        </label>
      </div>
      <div
        v-else-if="field.kind === 'mode'"
        role="radiogroup"
        :aria-labelledby="`${id}-label`"
        class="inline-flex flex-wrap gap-1 rounded-lg border p-1"
      >
        <label
          v-for="o in modes"
          :key="String(o.value)"
          class="rounded-md px-3 py-1"
          :class="[
            normalized === o.value ? 'bg-accent font-bold' : 'font-medium',
            disabled ? 'cursor-not-allowed opacity-70' : 'cursor-pointer',
          ]"
        >
          <input
            type="radio"
            class="sr-only"
            :name="id"
            :checked="normalized === o.value"
            :disabled="disabled"
            @change="$emit('set', field.path, o.value)"
          />{{ o.label }}
        </label>
      </div>
      <Textarea
        v-else-if="field.kind === 'list' || field.kind === 'textarea'"
        :id="id"
        :class="[boxClass, 'font-mono']"
        :rows="field.kind === 'list' ? 3 : 5"
        :disabled="disabled"
        :model-value="text"
        :aria-invalid="error ? true : undefined"
        @update:model-value="(v: string | number) => set(String(v))"
      />
      <ShadInput
        v-else
        :id="id"
        :class="[boxClass, field.uppercase ? 'uppercase' : '']"
        :type="inputType"
        :step="field.step"
        :placeholder="placeholder"
        :disabled="disabled"
        :model-value="text"
        :aria-invalid="error ? true : undefined"
        @update:model-value="(v: string | number) => set(String(v))"
      />
    </template>
    <p v-if="help" class="mt-1.5 text-sm text-muted-foreground">{{ help }}</p>
    <SettingNote
      :path="field.path"
      menu
      :default-text="defaultText"
      class="mt-1"
      @use-default="$emit('set', field.path, setting?.default)"
    />
    <p v-if="error" class="mt-1 text-sm text-destructive-foreground" data-test="field-error">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import SettingNote from '@/components/setup/SettingNote.vue';
import SourceChips from '@/components/setup/menu/SourceChips.vue';
import { Input as ShadInput } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import {
  defaultNote, fieldLocked, fromInput, inputValue, normalize, type FieldDef, type Read,
} from '@/lib/setupMenu';
import type { Setting } from '@/lib/setupState';

const props = defineProps<{
  field: FieldDef;
  value: unknown;
  read: Read;
  setting?: Setting;
  error?: string;
  secretSet?: boolean;
}>();

const emit = defineEmits<{ (e: 'set', path: string, value: unknown): void }>();

const id = computed(() => `setup-${props.field.path.replace(/\./g, '-')}`);
const locked = computed(() => fieldLocked(props.setting));
const disabled = computed(() => locked.value || (props.field.off?.(props.read) ?? false));
const normalized = computed(() => normalize(props.field, props.value));
const text = computed(() => inputValue(props.field, props.value));
const labelFor = computed(() => props.field.kind !== 'choice' && props.field.kind !== 'mode');
const help = computed(() => (typeof props.field.help === 'function' ? props.field.help(props.read) : props.field.help));
const defaultText = computed(() => defaultNote(props.field, props.setting, props.value));
const modes = [
  { value: false, label: 'Hotspot (simplex)' },
  { value: true, label: 'Repeater (duplex)' },
];

const inputType = computed(() => {
  switch (props.field.kind) {
    case 'secret':
      return 'password';
    case 'number':
    case 'seconds':
    case 'minutes':
    case 'mhz':
      return 'number';
    default:
      return 'text';
  }
});

const placeholder = computed(() => {
  if (props.field.kind === 'secret' && props.secretSet) {
    return '••••••••';
  }
  return props.field.placeholder;
});

const boxClass = 'w-full rounded-md border border-input px-3 py-1.5 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground disabled:opacity-100';

const isDefault = (v: string | number) => props.setting?.default !== undefined && normalize(props.field, props.setting.default) === v;

const set = (t: string) => emit('set', props.field.path, fromInput(props.field, t));
</script>
