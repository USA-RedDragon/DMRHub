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
  <div role="radiogroup" :aria-labelledby="labelledBy" class="inline-flex flex-wrap gap-1 rounded-lg border p-1 text-sm">
    <label
      v-for="o in options"
      :key="o.value"
      class="cursor-pointer rounded-md px-3 py-0.5"
      :class="modelValue === o.value ? 'bg-accent font-semibold text-foreground' : 'font-medium text-muted-foreground'"
    >
      <input
        type="radio"
        class="sr-only"
        :name="name"
        :value="o.value"
        :checked="modelValue === o.value"
        @change="$emit('update:modelValue', o.value)"
      />
      {{ o.label }}
    </label>
  </div>
</template>

<script setup lang="ts">
defineProps<{ name: string; labelledBy: string; options: { value: string; label: string }[]; modelValue: string }>();
defineEmits<{ 'update:modelValue': [value: string] }>();
</script>
