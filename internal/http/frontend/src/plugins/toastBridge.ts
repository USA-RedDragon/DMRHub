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

import type { App } from 'vue';
import { toast } from 'vue-sonner';

type ToastOptions = {
  severity?: string;
  summary?: string;
  detail?: string;
  life?: number;
};

const normalizeDuration = (life?: number) => {
  if (typeof life === 'number' && life > 0) {
    return life;
  }

  return 3000;
};

const showToast = (options: ToastOptions = {}) => {
  const title = options.summary || 'Notification';
  const description = options.detail;
  const duration = normalizeDuration(options.life);

  switch (options.severity) {
  case 'success':
    toast.success(title, { description, duration });
    return;
  case 'error':
    toast.error(title, { description, duration });
    return;
  case 'warn':
  case 'warning':
    toast.warning(title, { description, duration });
    return;
  case 'info':
    toast.info(title, { description, duration });
    return;
  default:
    toast(title, { description, duration });
  }
};

const toastAdapter = {
  add(options: ToastOptions = {}) {
    showToast(options);
  },
  removeAllGroups() {
    toast.dismiss();
  },
};

export default {
  install(app: App) {
    app.config.globalProperties.$toast = toastAdapter;
  },
};
