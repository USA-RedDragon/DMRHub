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
  <div v-if="codec" class="flex flex-col items-start gap-0.5">
    <span :class="badge" data-testid="codec-badge">{{ codec.label }}</span>
    <span v-for="(line, i) in codec.lines" :key="i" class="text-xs text-muted-foreground">{{ line }}</span>
  </div>
</template>

<script lang="ts">
import type { PropType } from 'vue';
import { badgeClass } from '@/lib/badge';
import { callCodec, type CallCodec as CallCodecView } from '@/lib/codec';
import { typeOf, type CallRow } from '@/lib/transmissions';

export default {
  name: 'CallCodec',
  props: {
    call: {
      type: Object as PropType<CallRow>,
      required: true,
    },
  },
  computed: {
    codec(): CallCodecView | null {
      return typeOf(this.call) === 'voice' ? callCodec(this.call) : null;
    },
    badge(): string {
      return this.codec ? badgeClass(this.codec.tone) : '';
    },
  },
};
</script>
