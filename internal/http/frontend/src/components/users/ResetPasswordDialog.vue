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
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent data-test="reset-password-dialog">
      <DialogTitle>Reset the password of {{ callsign }}</DialogTitle>
      <DialogDescription>
        DMRHub sets this temporary password and logs them out everywhere. Give it to them. They choose their own
        password when they log in with it.
      </DialogDescription>
      <div class="flex min-w-0 flex-col gap-1">
        <label for="reset-password" class="text-sm">Temporary password</label>
        <div class="flex items-center gap-2">
          <ShadInput id="reset-password" :model-value="password" class="font-mono" readonly />
          <ShadButton variant="outline" size="sm" type="button" data-test="reset-copy" @click="copy">
            Copy
          </ShadButton>
          <ShadButton variant="outline" size="sm" type="button" data-test="reset-new" @click="renew">New</ShadButton>
        </div>
      </div>
      <p v-if="copied" class="text-sm text-green-600 dark:text-green-500" data-test="reset-copied">Copied.</p>
      <div class="flex flex-wrap gap-2">
        <ShadButton type="button" :disabled="saving" data-test="reset-set" @click="set">Set password</ShadButton>
        <ShadButton variant="outline" type="button" data-test="reset-cancel" @click="emit('update:open', false)">
          Cancel
        </ShadButton>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Input as ShadInput } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { temporaryPassword } from '@/lib/password';
import { setUserPassword } from '@/services/password';
import { errorText } from '@/services/settings';

const props = defineProps<{ open: boolean; userId: number; callsign: string }>();
const emit = defineEmits<{ 'update:open': [value: boolean]; done: [] }>();

const password = ref(temporaryPassword());
const copied = ref(false);
const saving = ref(false);

const renew = () => {
  password.value = temporaryPassword();
  copied.value = false;
};

watch(
  () => props.open,
  (open) => {
    if (open) {
      renew();
    }
  },
);

const copy = async () => {
  try {
    await navigator.clipboard.writeText(password.value);
    copied.value = true;
  } catch {
    const input = document.getElementById('reset-password') as HTMLInputElement | null;
    input?.select();
  }
};

const set = async () => {
  saving.value = true;
  try {
    await setUserPassword(props.userId, password.value);
    emit('update:open', false);
    emit('done');
    toast.success(`${props.callsign}'s password is reset. They choose their own when they log in.`);
  } catch (err) {
    toast.error('Error', { description: errorText(err, `Error resetting the password of ${props.callsign}`) });
  } finally {
    saving.value = false;
  }
};
</script>
