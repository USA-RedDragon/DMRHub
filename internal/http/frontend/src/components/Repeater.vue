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
  <div class="inline-flex items-center gap-2">
    <TooltipProvider v-if="repeaterData">
      <Tooltip>
        <TooltipTrigger as-child>
          <RouterLink :to="`/repeaters/${repeater.id}`" class="text-primary underline">
            <span class="font-medium">{{ repeater.id }}</span>
          </RouterLink>
        </TooltipTrigger>
        <TooltipContent side="top" class="max-w-xs">
          <p class="font-semibold">{{ repeater.callsign }}</p>
          <p class="text-muted-foreground">{{ repeater.id }}</p>
          <p v-if="location" class="text-muted-foreground">{{ location }}</p>
          <p v-if="repeaterData.description" class="text-muted-foreground">{{ repeaterData.description }}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
    <div v-else>
      <RouterLink :to="`/repeaters/${repeater.id}`" class="text-primary underline font-medium">
        {{ repeater.callsign }}
      </RouterLink>
      <span class="text-muted-foreground text-sm">&nbsp;({{ repeater.id }})</span>
    </div>
  </div>
</template>

<script lang="ts">
import { RouterLink } from 'vue-router';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { getRepeater, type RepeaterData } from '@/services/repeater';

export default {
  name: 'RepeaterInfo',
  components: {
    RouterLink,
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  },
  props: {
    repeater: {
      type: Object as () => { id: number; callsign: string },
      required: true,
    },
  },
  data() {
    return {
      repeaterData: null as RepeaterData | null,
    };
  },
  computed: {
    location(): string {
      if (!this.repeaterData) return '';
      const parts = [this.repeaterData.city, this.repeaterData.state, this.repeaterData.country].filter(Boolean);
      return parts.join(', ');
    },
  },
  watch: {
    'repeater.id': {
      immediate: true,
      handler(newId: number) {
        if (newId) {
          getRepeater(newId).then((data) => {
            this.repeaterData = data;
          });
        }
      },
    },
  },
};
</script>
