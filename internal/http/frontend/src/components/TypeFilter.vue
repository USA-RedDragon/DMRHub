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
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2" aria-label="Filter by type">
      <span class="text-sm text-muted-foreground">Type:</span>
      <button
        v-for="t in types"
        :key="t.key"
        type="button"
        class="inline-flex h-[1.875rem] items-center gap-1.5 rounded-md border px-2.5 text-[0.8125rem] font-medium transition-colors hover:bg-accent"
        :class="isOn(t.key) ? ['text-foreground', colors[t.key].chip] : 'border-border bg-background text-muted-foreground'"
        :aria-pressed="isOn(t.key)"
        :data-kind="t.key"
        @click="toggle(t.key)"
      >
        <span class="size-2 rounded-full" :class="[colors[t.key].dot, isOn(t.key) ? 'opacity-100' : 'opacity-35']"></span>
        {{ t.label }}
      </button>
      <ShadButton variant="outline" size="sm" data-testid="type-all" @click="all">All</ShadButton>
    </div>
    <label class="inline-flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        class="size-4 accent-primary"
        :checked="modelValue.idle"
        data-testid="type-idle"
        @change="setIdle(($event.target as HTMLInputElement).checked)"
      />
      Show idle and USBD rows
    </label>
  </div>
</template>

<script lang="ts">
import type { PropType } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { TYPES, TYPE_COLORS, type TypeFilterValue, type TypeKey } from '@/lib/transmissions';

export default {
  name: 'TypeFilter',
  components: {
    ShadButton,
  },
  props: {
    modelValue: {
      type: Object as PropType<TypeFilterValue>,
      required: true,
    },
  },
  emits: ['update:modelValue'],
  data() {
    return {
      types: TYPES,
      colors: TYPE_COLORS,
    };
  },
  methods: {
    isOn(key: TypeKey): boolean {
      return this.modelValue.types.includes(key);
    },
    toggle(key: TypeKey) {
      const types = this.isOn(key)
        ? this.modelValue.types.filter((t) => t !== key)
        : TYPES.map((t) => t.key).filter((k) => k === key || this.modelValue.types.includes(k));
      this.$emit('update:modelValue', { ...this.modelValue, types });
    },
    all() {
      this.$emit('update:modelValue', { types: TYPES.map((t) => t.key), idle: true });
    },
    setIdle(idle: boolean) {
      this.$emit('update:modelValue', { ...this.modelValue, idle });
    },
  },
};
</script>
