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
      <template v-if="grouped">
        <optgroup v-for="g in groups" :key="g.replica" :label="`Replica ${replicaName(g.replica)}`">
          <option v-for="p in g.ports" :key="p.path" :value="key(g.replica, p.path)">{{ portLabel(p) }}</option>
        </optgroup>
      </template>
      <template v-else>
        <option v-for="p in ports" :key="p.path" :value="p.path">{{ portLabel(p) }}</option>
      </template>
      <option :value="OTHER">Another path</option>
    </select>
    <div v-if="typing" :class="['flex gap-2', ports.length > 0 ? 'mt-2' : '']">
      <ShadInput
        :id="ports.length > 0 ? `${id}-path` : id"
        :class="boxClass"
        :aria-label="ports.length > 0 ? 'Path of the serial port' : undefined"
        :placeholder="placeholder"
        :disabled="disabled"
        :model-value="value"
        :aria-invalid="invalid ? true : undefined"
        data-test="modem-port-path"
        @update:model-value="(v: string | number) => $emit('update', String(v))"
      />
      <select
        v-if="groups.length > 1"
        :class="[boxClass, 'h-9 w-auto bg-background']"
        :disabled="disabled"
        :value="portReplica"
        aria-label="Replica of the serial port"
        data-test="modem-port-replica"
        @change="portReplica = ($event.target as HTMLSelectElement).value"
      >
        <option v-for="g in groups" :key="g.replica" :value="g.replica">Replica {{ replicaName(g.replica) }}</option>
      </select>
    </div>
    <p class="mt-1.5 text-sm text-muted-foreground">
      The serial device of the board.<template v-if="ports.length > 0"> The list shows the ports that {{ grouped ? 'each replica' : 'this server' }} finds.</template>
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Input as ShadInput } from '@/components/ui/input';
import { otherPort, portLabel, portReplica, portsOf, replicaOfPort, type ReplicaPorts } from '@/lib/modem';
import { replicaName } from '@/lib/setupState';
import API from '@/services/API';

const OTHER = '\u0000other';
const SEP = '\u0001';

const props = defineProps<{ id: string; value: string; disabled?: boolean; invalid?: boolean; placeholder?: string; boxClass: string }>();
const emit = defineEmits<{ (e: 'update', value: string): void }>();

const groups = ref<ReplicaPorts[]>([]);
const chose = ref(false);

const ports = computed(() => groups.value.flatMap((g) => g.ports));
const grouped = computed(() => groups.value.length > 1);
const typing = computed(() => chose.value || otherPort(groups.value, props.value));
const key = (replica: string, path: string) => `${replica}${SEP}${path}`;
const choice = computed(() => {
  if (typing.value) {
    return OTHER;
  }
  return grouped.value && props.value !== '' ? key(portReplica.value, props.value) : props.value;
});

const pick = (v: string) => {
  if (v === OTHER) {
    chose.value = true;
    return;
  }
  chose.value = false;
  if (grouped.value) {
    const [replica = '', path = ''] = v.split(SEP);
    portReplica.value = replica;
    emit('update', path);
    return;
  }
  emit('update', v);
};

onMounted(async () => {
  portReplica.value = '';
  try {
    const res = await API.get('/config/modem/ports');
    groups.value = portsOf(res.data);
  } catch {
    groups.value = [];
  }
  if (grouped.value) {
    portReplica.value = replicaOfPort(groups.value, props.value) || (groups.value.find((g) => g.self)?.replica ?? '');
  }
});
</script>
