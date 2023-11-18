// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
import { VueHeadMixin, createHead } from '@unhead/vue';

import PrimeVue from 'primevue/config';
import ToastService from 'primevue/toastservice';
import DialogService from 'primevue/dialogservice';
import ConfirmationService from 'primevue/confirmationservice';
import Toast from 'primevue/toast';
import ConfirmDialog from 'primevue/confirmdialog';

import App from './App.vue';
import router from './router';
import features from './services/features';

import 'primeflex/primeflex.scss';
import 'primeicons/primeicons.css';
import 'primevue/resources/primevue.min.css';

import './assets/main.css';

const pinia = createPinia();
const app = createApp(App);
app.mixin(VueHeadMixin);
const head = createHead();
app.use(head);
app.use(ToastService);
app.use(DialogService);
app.use(ConfirmationService);
app.use(pinia);
app.use(PrimeVue);

app.component('PVToast', Toast);
app.component('ConfirmDialog', ConfirmDialog);

features.getFeatures().then(() => {
  app.use(router(features));
}).catch(() => {
  app.use(router({
    OpenBridge: '',
    isEnabled: () => {
      return false;
    },
  }));
}).finally(() => {
  app.mount('#app');
});
