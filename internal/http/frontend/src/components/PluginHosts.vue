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
  <div class="flex flex-col gap-1.5">
    <span class="text-sm font-medium">Network</span>
    <p class="text-sm text-muted-foreground">{{ intro }}</p>
    <div v-for="(h, i) in hosts" :key="h.address" class="flex flex-col gap-0.5">
      <label class="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          class="size-4 accent-primary"
          :data-test="`host-${i}`"
          :checked="modelValue.includes(h.address)"
          @change="toggle(h.address, ($event.target as HTMLInputElement).checked)"
        />
        <span class="font-mono">{{ h.address }}</span>
      </label>
      <p v-if="h.reason" class="ml-6 text-sm text-muted-foreground">{{ h.reason }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { PluginHost } from '@/services/plugins';

const props = defineProps<{ hosts: PluginHost[]; modelValue: string[]; intro: string }>();

const emit = defineEmits<{
  (e: 'update:modelValue', value: string[]): void
}>();

const toggle = (address: string, on: boolean) => {
  const rest = props.modelValue.filter((a) => a !== address);
  emit('update:modelValue', on ? [...rest, address] : rest);
};
</script>
