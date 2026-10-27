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
  <span
    class="inline-flex items-center gap-1.5 rounded-full border px-2.5 text-xs leading-[22px] font-normal whitespace-nowrap"
    :title="pill.title"
    data-test="modem-pill"
  >
    <span class="size-[7px] rounded-full" :class="dotClass(pill.tone)" />{{ pill.text }}
  </span>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { modemPill, modemStateOf, type ModemState, type Tone } from '@/lib/modem';
import API from '@/services/API';

const REFRESH_MS = 5000;

const state = ref<ModemState | null>(null);
const pill = computed(() => modemPill(state.value));

const dotClass = (tone: Tone) => {
  switch (tone) {
    case 'ok':
      return 'bg-green-600 dark:bg-green-400';
    case 'bad':
      return 'bg-destructive-foreground';
    default:
      return 'bg-muted-foreground';
  }
};

let timer: ReturnType<typeof setInterval> | null = null;

const refresh = async () => {
  try {
    const res = await API.get('/config/modem');
    state.value = modemStateOf(res.data);
  } catch {
    state.value = null;
  }
};

onMounted(() => {
  refresh();
  timer = setInterval(refresh, REFRESH_MS);
});

onUnmounted(() => {
  if (timer !== null) {
    clearInterval(timer);
    timer = null;
  }
});
</script>
