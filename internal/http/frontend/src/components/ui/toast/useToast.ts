// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

import { ref } from 'vue';

export type ToastVariant = 'default' | 'success' | 'error' | 'warning' | 'info';

export interface Toast {
  id: number;
  title: string;
  description?: string;
  variant: ToastVariant;
  duration: number;
  dismissing?: boolean;
}

const toasts = ref<Toast[]>([]);
let nextId = 0;

const TOAST_LIMIT = 5;
const DEFAULT_DURATION = 4000;

function addToast(toast: Omit<Toast, 'id'>) {
  const id = nextId++;
  toasts.value.push({ ...toast, id });

  if (toasts.value.length > TOAST_LIMIT) {
    toasts.value.splice(0, toasts.value.length - TOAST_LIMIT);
  }

  if (toast.duration > 0) {
    setTimeout(() => {
      dismiss(id);
    }, toast.duration);
  }

  return id;
}

function dismiss(id: number) {
  const toastItem = toasts.value.find((t) => t.id === id);
  if (toastItem) {
    toastItem.dismissing = true;
    setTimeout(() => {
      toasts.value = toasts.value.filter((t) => t.id !== id);
    }, 200);
  }
}

function dismissAll() {
  toasts.value.forEach((t) => {
    t.dismissing = true;
  });
  setTimeout(() => {
    toasts.value = [];
  }, 200);
}

export const toast = {
  success(title: string, options?: { description?: string; duration?: number }) {
    return addToast({
      title,
      description: options?.description,
      variant: 'success',
      duration: options?.duration ?? DEFAULT_DURATION,
    });
  },
  error(title: string, options?: { description?: string; duration?: number }) {
    return addToast({
      title,
      description: options?.description,
      variant: 'error',
      duration: options?.duration ?? DEFAULT_DURATION,
    });
  },
  info(title: string, options?: { description?: string; duration?: number }) {
    return addToast({
      title,
      description: options?.description,
      variant: 'info',
      duration: options?.duration ?? DEFAULT_DURATION,
    });
  },
  warning(title: string, options?: { description?: string; duration?: number }) {
    return addToast({
      title,
      description: options?.description,
      variant: 'warning',
      duration: options?.duration ?? DEFAULT_DURATION,
    });
  },
  dismiss: dismissAll,
};

export function useToast() {
  return {
    toasts,
    toast,
    dismiss,
    dismissAll,
  };
}
