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
  <div v-if="read('dmr.routing.spoken-refusals') === true" class="mb-4 overflow-x-auto rounded-md border" data-test="prompt-list">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>When</TableHead>
          <TableHead>The radio hears</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="r in shown" :key="r.clip" :data-test="`prompt-${r.clip}`">
          <TableCell class="whitespace-normal">{{ r.when }}</TableCell>
          <TableCell>
            <span class="inline-flex items-center gap-2">
              <ShadButton
                type="button"
                variant="outline"
                size="sm"
                :aria-label="`Play ${r.text}`"
                data-test="prompt-play"
                @click="play(r.clip)"
              >
                Play
              </ShadButton>
              "{{ r.text }}"
            </span>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <p v-if="error" class="px-3 py-2 text-sm text-destructive" data-test="prompt-error">{{ error }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { playPrompt, SPOKEN_REFUSALS } from '@/lib/prompts';
import type { Read } from '@/lib/setupMenu';
import { setupPreviewURL } from '@/services/prompts';

const props = defineProps<{ read: Read }>();
const error = ref('');
const shown = computed(() => SPOKEN_REFUSALS.filter((r) => !r.applies || r.applies(props.read)));

const play = async (clip: string) => {
  error.value = '';
  const voice = props.read('dmr.routing.prompt-voice');
  try {
    await playPrompt(setupPreviewURL(clip, typeof voice === 'string' ? voice : undefined));
  } catch {
    error.value = 'Could not play the prompt.';
  }
};
</script>
