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
  <Dialog :open="open" @update:open="close">
    <DialogContent data-test="require-password-dialog">
      <template v-if="password === ''">
        <DialogTitle>Require a new password from {{ callsign }}</DialogTitle>
        <DialogDescription>
          DMRHub logs {{ callsign }} out everywhere. They must choose a new password when they next log in.
        </DialogDescription>
        <div class="flex items-center gap-2">
          <Checkbox id="require-generate" v-model="generate" data-test="require-generate" />
          <label for="require-generate" class="text-sm">Generate a random password</label>
        </div>
        <p v-if="generate" class="text-sm text-muted-foreground">
          Their current password stops working. You give them the new one.
        </p>
        <div class="flex flex-wrap gap-2">
          <ShadButton type="button" :disabled="saving" data-test="require-confirm" @click="confirm">
            Require password change
          </ShadButton>
          <ShadButton variant="outline" type="button" data-test="require-cancel" @click="close(false)">
            Cancel
          </ShadButton>
        </div>
      </template>
      <template v-else>
        <DialogTitle>New password of {{ callsign }}</DialogTitle>
        <DialogDescription>
          Give this password to {{ callsign }}. It will not be shown again. They choose their own password when they
          log in with it.
        </DialogDescription>
        <div class="flex min-w-0 flex-col gap-1">
          <label for="require-password" class="text-sm">Temporary password</label>
          <div class="flex items-center gap-2">
            <ShadInput id="require-password" :model-value="password" class="font-mono" readonly />
            <ShadButton variant="outline" size="sm" type="button" data-test="require-copy" @click="copy">
              Copy
            </ShadButton>
          </div>
        </div>
        <p v-if="copied" class="text-sm text-green-600 dark:text-green-500" data-test="require-copied">Copied.</p>
        <div class="flex flex-wrap gap-2">
          <ShadButton type="button" data-test="require-done" @click="close(false)">Done</ShadButton>
        </div>
      </template>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input as ShadInput } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { requirePasswordChange } from '@/services/password';
import { errorText } from '@/services/settings';

const props = defineProps<{ open: boolean; userId: number; callsign: string }>();
const emit = defineEmits<{ 'update:open': [value: boolean]; done: [] }>();

const generate = ref(false);
const password = ref('');
const copied = ref(false);
const saving = ref(false);

const reset = () => {
  generate.value = false;
  password.value = '';
  copied.value = false;
};

watch(
  () => props.open,
  (open) => {
    if (open) {
      reset();
    }
  },
);

const close = (open: boolean) => {
  if (!open) {
    reset();
  }
  emit('update:open', open);
};

const copy = async () => {
  try {
    await navigator.clipboard.writeText(password.value);
    copied.value = true;
  } catch {
    const input = document.getElementById('require-password') as HTMLInputElement | null;
    input?.select();
  }
};

const confirm = async () => {
  saving.value = true;
  try {
    const generated = await requirePasswordChange(props.userId, generate.value);
    emit('done');
    toast.success(`${props.callsign} must choose a new password when they next log in.`);
    if (generated) {
      password.value = generated;
    } else {
      close(false);
    }
  } catch (err) {
    toast.error('Error', { description: errorText(err, `Error requiring a new password from ${props.callsign}`) });
  } finally {
    saving.value = false;
  }
};
</script>
