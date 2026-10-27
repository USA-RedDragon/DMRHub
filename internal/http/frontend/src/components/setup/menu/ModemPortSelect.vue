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
  <div data-test="modem-port">
    <select
      v-if="ports.length > 0"
      :id="id"
      :class="[boxClass, 'h-9 bg-background']"
      :disabled="disabled"
      :value="choice"
      :aria-invalid="invalid ? true : undefined"
      data-test="modem-port-select"
      @change="pick(($event.target as HTMLSelectElement).value)"
    >
      <option v-if="value === '' && !typing" value="" disabled>Choose a port</option>
      <option v-for="p in ports" :key="p.path" :value="p.path">{{ portLabel(p) }}</option>
      <option :value="OTHER">Another path</option>
    </select>
    <ShadInput
      v-if="typing"
      :id="ports.length > 0 ? `${id}-path` : id"
      :class="[boxClass, ports.length > 0 ? 'mt-2' : '']"
      :aria-label="ports.length > 0 ? 'Path of the serial port' : undefined"
      :placeholder="placeholder"
      :disabled="disabled"
      :model-value="value"
      :aria-invalid="invalid ? true : undefined"
      data-test="modem-port-path"
      @update:model-value="(v: string | number) => $emit('update', String(v))"
    />
    <p class="mt-1.5 text-sm text-muted-foreground">
      The serial device of the board.<template v-if="ports.length > 0"> The list shows the ports that this server finds.</template>
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Input as ShadInput } from '@/components/ui/input';
import { otherPort, portLabel, portsOf, type SerialPort } from '@/lib/modem';
import API from '@/services/API';

const OTHER = '\u0000other';

const props = defineProps<{ id: string; value: string; disabled?: boolean; invalid?: boolean; placeholder?: string; boxClass: string }>();
const emit = defineEmits<{ (e: 'update', value: string): void }>();

const ports = ref<SerialPort[]>([]);
const chose = ref(false);

const typing = computed(() => chose.value || otherPort(ports.value, props.value));
const choice = computed(() => (typing.value ? OTHER : props.value));

const pick = (v: string) => {
  if (v === OTHER) {
    chose.value = true;
    return;
  }
  chose.value = false;
  emit('update', v);
};

onMounted(async () => {
  try {
    const res = await API.get('/config/modem/ports');
    ports.value = portsOf(res.data);
  } catch {
    ports.value = [];
  }
});
</script>
