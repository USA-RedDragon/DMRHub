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
  <div class="mode-toggle" :class="{ compact }">
    <DropdownMenuRoot :modal="false">
      <DropdownMenuTrigger as-child>
        <button class="mode-trigger" type="button" aria-label="Toggle theme">
          <Sun v-if="mode === 'light'" :size="16" />
          <Moon v-else-if="mode === 'dark'" :size="16" />
          <Monitor v-else :size="16" />
          <span class="mode-label">Theme: {{ modeLabel }}</span>
        </button>
      </DropdownMenuTrigger>

      <DropdownMenuContent class="mode-menu" :side-offset="8" align="end">
        <DropdownMenuItem class="mode-menu-item" @click="setMode('light')">
          <Sun :size="14" />
          <span>Light</span>
        </DropdownMenuItem>
        <DropdownMenuItem class="mode-menu-item" @click="setMode('dark')">
          <Moon :size="14" />
          <span>Dark</span>
        </DropdownMenuItem>
        <DropdownMenuItem class="mode-menu-item" @click="setMode('system')">
          <Monitor :size="14" />
          <span>System</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuRoot>
  </div>
</template>

<script setup lang="ts">
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from 'reka-ui';
import { Monitor, Moon, Sun } from 'lucide-vue-next';
import { useColorMode } from '@/composables/useColorMode';

defineProps({
  compact: {
    type: Boolean,
    default: false,
  },
});

const { mode, modeLabel, setMode } = useColorMode();
</script>

<style scoped>
.mode-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}

.mode-toggle.compact {
  margin-top: 0.4rem;
}

.mode-trigger {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  background-color: transparent;
  color: var(--foreground);
  border: 1px solid var(--border);
  border-radius: 0.35rem;
  padding: 0.25rem 0.5rem;
  cursor: pointer;
}

.mode-label {
  font-size: 0.9em;
  font-weight: 600;
}
</style>

<style>
.mode-menu {
  min-width: 9rem !important;
  background: var(--popover);
  color: var(--popover-foreground);
  border: 1px solid var(--border);
  border-radius: 0.35rem;
  padding: 0.25rem;
  z-index: 50;
}

.mode-menu-item {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  cursor: pointer;
  color: var(--popover-foreground);
  border-radius: 0.3rem;
  padding: 0.35rem 0.45rem;
}

.mode-menu-item:hover {
  background: var(--accent);
}
</style>
