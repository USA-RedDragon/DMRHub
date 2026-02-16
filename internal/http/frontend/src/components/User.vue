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
    <TooltipProvider v-if="radioIdData">
      <Tooltip>
        <TooltipTrigger as-child>
          <div>
            <span v-if="radioIdData.flag" class="cursor-default text-base leading-none">{{ radioIdData.flag }}&nbsp;</span>
            <span class="font-medium">{{ user.callsign }}</span>
          </div>
        </TooltipTrigger>
        <TooltipContent side="top" class="max-w-xs">
          <p class="font-semibold">{{ radioIdData.name }} {{ radioIdData.surname }}</p>
          <p class="text-muted-foreground">{{ user.id }}</p>
          <p v-if="location" class="text-muted-foreground">{{ location }}</p>
          <p v-if="radioIdData.country" class="text-muted-foreground">{{ radioIdData.flag }} {{ radioIdData.country }}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
    <div v-else>
      <span class="font-medium">{{ user.callsign }}</span>
      <span class="text-muted-foreground text-sm">&nbsp;({{ user.id }})</span>
    </div>
  </div>
</template>

<script lang="ts">
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { getUserDB, type RadioIdData } from '@/services/userdb';

export default {
  name: 'UserInfo',
  components: {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  },
  props: {
    user: {
      type: Object as () => { id: number; callsign: string },
      required: true,
    },
  },
  data() {
    return {
      radioIdData: null as RadioIdData | null,
    };
  },
  computed: {
    location(): string {
      if (!this.radioIdData) return '';
      const parts = [this.radioIdData.city, this.radioIdData.state].filter(Boolean);
      return parts.join(', ');
    },
  },
  watch: {
    'user.id': {
      immediate: true,
      handler(newId: number) {
        if (newId) {
          getUserDB(newId).then((data) => {
            this.radioIdData = data;
          });
        }
      },
    },
  },
};
</script>
