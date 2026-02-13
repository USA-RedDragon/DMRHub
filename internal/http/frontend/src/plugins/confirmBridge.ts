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
import { defineComponent, h } from 'vue';

type ConfirmCallback = (() => void) | undefined;

type ConfirmOptions = {
  header?: string;
  message?: string;
  rejectClass?: string;
  rejectLabel?: string;
  accept?: ConfirmCallback;
  reject?: ConfirmCallback;
};

const ConfirmDialogStub = defineComponent({
  name: 'ConfirmDialog',
  setup() {
    return () => h('div', { style: 'display:none' });
  },
});

const runCallback = (callback: ConfirmCallback) => {
  if (typeof callback === 'function') {
    callback();
  }
};

const asMessage = (options: ConfirmOptions) => {
  const header = options.header ? `${options.header}\n\n` : '';
  const message = options.message || 'Are you sure?';
  return `${header}${message}`;
};

const shouldUseAlertOnly = (options: ConfirmOptions) => {
  if (options.rejectClass === 'remove-reject-button') {
    return true;
  }

  if (!options.reject && !options.rejectLabel) {
    return true;
  }

  return false;
};

const confirmAdapter = {
  require(options: ConfirmOptions = {}) {
    const message = asMessage(options);

    if (shouldUseAlertOnly(options)) {
      window.alert(message);
      runCallback(options.accept);
      return;
    }

    if (window.confirm(message)) {
      runCallback(options.accept);
      return;
    }

    runCallback(options.reject);
  },
};

export default {
  install(app: App) {
    app.config.globalProperties.$confirm = confirmAdapter;
    app.component('ConfirmDialog', ConfirmDialogStub);
  },
};
