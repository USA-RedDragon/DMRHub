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
  <ShadButton type="button" variant="outline" size="sm" class="h-7 px-2.5 text-[13px]" :data-copy="text" @click="copy">
    {{ label }}
  </ShadButton>
</template>

<script setup lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import { toast } from '@/components/ui/toast';

const props = withDefaults(defineProps<{ text: string | number; label?: string }>(), { label: 'Copy' });

const copy = async () => {
  try {
    await navigator.clipboard.writeText(String(props.text));
    toast.success('Copied');
  } catch {
    toast.error('Could not copy. Select the text and copy it.');
  }
};
</script>
