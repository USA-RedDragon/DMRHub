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

import { createApp } from 'vue';
import { createPinia } from 'pinia';
import { VueHeadMixin } from '@unhead/vue';
import { createHead } from '@unhead/vue/client';

import ConfirmBridge from './plugins/confirmBridge.js';
import ToastBridge from './plugins/toastBridge.js';

import App from './App.vue';
import router from './router';
import API from './services/API';

import 'vue-sonner/style.css';

import './assets/main.css';

const pinia = createPinia();
const app = createApp(App);
app.mixin(VueHeadMixin);
const head = createHead();
app.use(head);
app.use(ConfirmBridge);
app.use(ToastBridge);
app.use(pinia);

API.get('/setupwizard').then((response) => {
  if (response.data && response.data.setupwizard) {
    console.error('Entering setup wizard mode');
    app.use(router(response.data.setupwizard));
  } else {
    app.use(router(false));
  }
}).catch(() => {
  app.use(router(false));
}).finally(() => {
  app.mount('#app');
});
