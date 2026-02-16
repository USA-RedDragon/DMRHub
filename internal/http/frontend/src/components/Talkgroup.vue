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
    <TooltipProvider v-if="talkgroupData">
      <Tooltip>
        <TooltipTrigger as-child>
          <p class="font-medium">{{ talkgroupData.name }}</p>
        </TooltipTrigger>
        <TooltipContent side="top" class="max-w-xs">
          <p class="font-semibold">TG {{ talkgroup.id }} - {{ talkgroupData.name }}</p>
          <p class="text-muted-foreground">{{ talkgroupData.description }}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
    <div v-else>
      <p class="font-medium">TG {{ talkgroup.id }}</p>
    </div>
  </div>
</template>

<script lang="ts">
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { getTalkgroup, type TalkgroupData } from '@/services/talkgroup';

export default {
  name: 'TalkgroupInfo',
  components: {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  },
  props: {
    talkgroup: {
      type: Object as () => { id: number },
      required: true,
    },
  },
  data() {
    return {
      talkgroupData: null as TalkgroupData | null,
    };
  },
  watch: {
    'talkgroup.id': {
      immediate: true,
      handler(newId: number) {
        if (newId) {
          getTalkgroup(newId).then((data) => {
            this.talkgroupData = data;
          });
        }
      },
    },
  },
};
</script>
