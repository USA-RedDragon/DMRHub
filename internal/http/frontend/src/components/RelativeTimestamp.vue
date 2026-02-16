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
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger as-child>
          <span v-if="active">Active</span>
          <span v-else>{{ relativeTime }}</span>
        </TooltipTrigger>
        <TooltipContent side="top" class="max-w-xs">
          <p>{{ absoluteTime }}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  </div>
</template>

<script lang="ts">
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { format, formatDistanceToNowStrict } from 'date-fns';

export default {
  name: 'RelativeTimestamp',
  components: {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  },
  props: {
    time: {
      type: [String, Date],
      required: true,
    },
    active: {
      type: Boolean,
      required: false,
      default: false,
    }
  },
  data() {
    return {
    };
  },
  computed: {
    relativeTime(): string {
      const date = typeof this.time === 'string' ? new Date(this.time) : this.time;
      return formatDistanceToNowStrict(date, { addSuffix: true });
    },
    absoluteTime(): string {
      const date = typeof this.time === 'string' ? new Date(this.time) : this.time;
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return format(date, 'yyyy-MM-dd HH:mm:ss');
    },
  },
};
</script>
