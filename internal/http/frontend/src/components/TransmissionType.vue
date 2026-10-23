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
  <div class="flex flex-col items-start gap-0.5">
    <span
      class="inline-flex items-center whitespace-nowrap rounded-md px-2.5 py-0.5 text-xs font-medium"
      :class="colors.badge"
      data-testid="type-badge"
    >{{ label }}</span>
    <span v-for="(line, i) in lines" :key="i" class="text-xs text-muted-foreground">{{ line }}</span>
  </div>
</template>

<script lang="ts">
import type { PropType } from 'vue';
import { TYPE_COLORS, typeLabel, typeLines, typeOf, type CallRow } from '@/lib/transmissions';

export default {
  name: 'TransmissionType',
  props: {
    call: {
      type: Object as PropType<CallRow>,
      required: true,
    },
  },
  computed: {
    type() {
      return typeOf(this.call);
    },
    label(): string {
      return typeLabel(this.type);
    },
    lines(): string[] {
      return typeLines(this.call);
    },
    colors() {
      return TYPE_COLORS[this.type];
    },
  },
};
</script>
