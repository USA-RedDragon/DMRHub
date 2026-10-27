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
  <div class="mb-3.5 flex flex-wrap items-start gap-2.5 text-[13px] text-muted-foreground">
    <span
      v-if="tested"
      class="inline-flex items-center gap-1.5 rounded-full border border-success/55 px-2.5 text-xs leading-[22px] font-medium whitespace-nowrap text-foreground"
    ><span class="size-[7px] shrink-0 rounded-full bg-success" />Tested</span>
    <span
      v-else
      class="inline-flex items-center gap-1.5 rounded-full border border-warn-border bg-warn-bg px-2.5 text-xs leading-[22px] font-medium whitespace-nowrap text-warn-fg"
    ><span class="size-[7px] shrink-0 rounded-full bg-warn-border" />Not tested</span>
    <span v-if="text">{{ text }}</span>
  </div>
</template>

<script setup lang="ts">
defineProps<{ tested: boolean; text?: string }>();
</script>
