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
  <Card data-test="prompt-voice-card">
    <CardHeader>
      <CardTitle>Spoken prompts</CardTitle>
      <CardDescription>The voice that tells your radio why DMRHub refused a call.</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <label for="prompt-voice" class="text-sm font-medium">Voice</label>
        <select
          id="prompt-voice"
          :value="voice"
          :disabled="saving || voices.length === 0"
          class="h-9 rounded-md border border-input bg-background px-3 py-1.5 text-sm"
          data-test="prompt-voice"
          @change="save(($event.target as HTMLSelectElement).value)"
        >
          <option value="">Network default ({{ nameOf(defaultVoice) }})</option>
          <option v-for="v in voices" :key="v.id" :value="v.id">{{ v.name }}</option>
        </select>
        <ShadButton type="button" variant="outline" size="sm" data-test="prompt-voice-play" @click="play">Play</ShadButton>
      </div>
      <p v-if="error" class="text-sm text-destructive" data-test="prompt-voice-error">{{ error }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { playPrompt } from '@/lib/prompts';
import { getPrompts, previewURL, setPromptVoice, type PromptVoice } from '@/services/prompts';
import { errorText } from '@/services/settings';

const props = defineProps<{ voice: string; defaultVoice: string }>();
const voice = ref(props.voice);
const defaultVoice = ref(props.defaultVoice);
const voices = ref<PromptVoice[]>([]);
const saving = ref(false);
const error = ref('');

const nameOf = (id: string) => voices.value.find((v) => v.id === id)?.name ?? id;

onMounted(async () => {
  try {
    const list = await getPrompts();
    voices.value = list.voices;
    defaultVoice.value = list.default;
  } catch (err) {
    error.value = errorText(err, 'Could not load the voices.');
  }
});

const save = async (value: string) => {
  error.value = '';
  saving.value = true;
  try {
    const res = await setPromptVoice(value);
    voice.value = res.prompt_voice ?? value;
  } catch (err) {
    error.value = errorText(err, 'Could not save the voice.');
  } finally {
    saving.value = false;
  }
};

const play = async () => {
  error.value = '';
  try {
    await playPrompt(previewURL(voice.value || defaultVoice.value, 'talkgroup-not-found'));
  } catch {
    error.value = 'Could not play the prompt.';
  }
};
</script>
