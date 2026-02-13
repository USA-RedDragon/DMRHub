<!-- eslint-disable vue/multi-word-component-names -->
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

<script setup lang="ts">
import { useToast, type Toast } from './useToast';
import { cn } from '@/lib/utils';
import { X, CheckCircle2, XCircle, AlertTriangle, Info } from 'lucide-vue-next';

const { toasts, dismiss } = useToast();

const variantClasses: Record<Toast['variant'], string> = {
  default: 'border-border bg-background text-foreground',
  success: 'border-green-500/30 bg-green-50 text-green-900 dark:bg-green-950 dark:text-green-100 dark:border-green-500/30',
  error: 'border-red-500/30 bg-red-50 text-red-900 dark:bg-red-950 dark:text-red-100 dark:border-red-500/30',
  warning: 'border-yellow-500/30 bg-yellow-50 text-yellow-900 dark:bg-yellow-950 dark:text-yellow-100 dark:border-yellow-500/30',
  info: 'border-blue-500/30 bg-blue-50 text-blue-900 dark:bg-blue-950 dark:text-blue-100 dark:border-blue-500/30',
};

const iconComponents: Record<Toast['variant'], typeof CheckCircle2 | null> = {
  default: null,
  success: CheckCircle2,
  error: XCircle,
  warning: AlertTriangle,
  info: Info,
};

const iconClasses: Record<Toast['variant'], string> = {
  default: '',
  success: 'text-green-600 dark:text-green-400',
  error: 'text-red-600 dark:text-red-400',
  warning: 'text-yellow-600 dark:text-yellow-400',
  info: 'text-blue-600 dark:text-blue-400',
};
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed bottom-0 right-0 z-[100] flex max-h-screen w-full flex-col-reverse gap-2 p-4 sm:max-w-[420px]"
    >
      <TransitionGroup
        enter-active-class="transition-all duration-300 ease-out"
        enter-from-class="translate-y-2 opacity-0"
        enter-to-class="translate-y-0 opacity-100"
        leave-active-class="transition-all duration-200 ease-in"
        leave-from-class="translate-y-0 opacity-100"
        leave-to-class="translate-x-full opacity-0"
      >
        <div
          v-for="t in toasts"
          :key="t.id"
          :class="cn(
            'group pointer-events-auto relative flex w-full items-start gap-3 overflow-hidden rounded-lg border p-4 shadow-lg',
            variantClasses[t.variant],
            t.dismissing && 'translate-x-full opacity-0',
          )"
          role="alert"
        >
          <component
            :is="iconComponents[t.variant]"
            v-if="iconComponents[t.variant]"
            :class="cn('mt-0.5 size-5 shrink-0', iconClasses[t.variant])"
          />

          <div class="flex-1 space-y-1">
            <p class="text-sm font-semibold leading-none tracking-tight">
              {{ t.title }}
            </p>
            <p v-if="t.description" class="text-sm opacity-80">
              {{ t.description }}
            </p>
          </div>

          <button
            class="absolute top-2 right-2 rounded-md p-1 opacity-0 transition-opacity hover:opacity-100 group-hover:opacity-70 focus:opacity-100 focus:outline-none"
            @click="dismiss(t.id)"
          >
            <X class="size-4" />
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>
