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
  <div class="mb-3.5 overflow-x-auto rounded-[10px] border">
    <table class="w-full border-collapse text-sm">
      <thead>
        <tr>
          <th
            v-for="h in head"
            :key="h"
            class="border-b bg-stripe px-3 py-2 text-left font-semibold whitespace-nowrap text-muted-foreground"
          >{{ h }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, r) in rows" :key="r" class="[&:last-child>td]:border-b-0">
          <td
            v-for="(cell, c) in row"
            :key="c"
            class="border-b px-3 py-2 align-top"
            :class="c === row.length - 1 && row.length > 1 ? 'font-mono text-[15px] whitespace-nowrap' : ''"
          ><span v-if="typeof cell === 'object'" class="rounded-[3px] bg-primary/18 px-0.5">{{ cell.v }}</span><template v-else>{{ cell }}</template></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import type { Cell } from '@/lib/enroll';

defineProps<{ head: string[]; rows: Cell[][] }>();
</script>
