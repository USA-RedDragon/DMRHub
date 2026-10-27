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
  <div class="mb-3.5 rounded-[10px] border bg-stripe">
    <div class="flex items-center justify-between gap-2 border-b py-1.5 pr-2 pl-3.5 text-xs font-semibold text-muted-foreground">
      <span>{{ title }}</span>
      <CopyButton :text="snippetText(segments)" />
    </div>
    <pre class="m-0 overflow-x-auto px-3.5 py-3 font-mono text-[15px] leading-[1.45]"><template v-for="(s, i) in segments" :key="i"><span v-if="s.hl" class="rounded-[3px] bg-primary/18 px-0.5">{{ s.text }}</span><template v-else>{{ s.text }}</template></template></pre>
  </div>
</template>

<script setup lang="ts">
import { snippetText, type Segment } from '@/lib/enroll';
import CopyButton from './CopyButton.vue';

defineProps<{ title: string; segments: Segment[] }>();
</script>
