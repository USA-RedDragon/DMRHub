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

import { defineStore } from 'pinia';

interface UserState {
  loggedIn: boolean;
  id: number;
  callsign: string;
  username: string;
  admin: boolean;
  superAdmin: boolean;
  created_at: string;
  hasOpenBridgePeers: boolean;
  mustChangePassword: boolean;
}

interface SettingsState {
  ipscEnabled: boolean;
  openBridgeEnabled: boolean;
  passwordResetEnabled: boolean;
  radioIDValidation: boolean;
  registration: string;
  visibility: string;
  mmdvmPort: number;
  ipscPort: number;
  openBridgePort: number;
  serverAddress: string;
  loaded: boolean;
}

export const useUserStore = defineStore('user', {
  state: (): UserState => ({
    loggedIn: false,
    id: 0,
    callsign: '',
    username: '',
    admin: false,
    superAdmin: false,
    created_at: '',
    hasOpenBridgePeers: false,
    mustChangePassword: false,
  }),
  getters: {},
  actions: {},
});

export const useSettingsStore = defineStore('settings', {
  state: (): SettingsState => ({
    ipscEnabled: false,
    openBridgeEnabled: false,
    passwordResetEnabled: false,
    radioIDValidation: false,
    registration: 'open',
    visibility: 'public',
    mmdvmPort: 62031,
    ipscPort: 50000,
    openBridgePort: 62035,
    serverAddress: '',
    loaded: false,
  }),
  getters: {},
  actions: {
    async fetchConfig() {
      if (this.loaded) return;
      try {
        const { default: API } = await import('@/services/API');
        const res = await API.get('/network/features');
        this.ipscEnabled = res.data?.ipsc ?? false;
        this.openBridgeEnabled = res.data?.openbridge ?? false;
        this.passwordResetEnabled = res.data?.password_reset ?? false;
        this.radioIDValidation = res.data?.radio_id_validation ?? false;
        this.registration = res.data?.registration ?? 'open';
        this.visibility = res.data?.visibility ?? 'public';
        this.mmdvmPort = res.data?.mmdvm_port || 62031;
        this.ipscPort = res.data?.ipsc_port || 50000;
        this.openBridgePort = res.data?.openbridge_port || 62035;
        this.serverAddress = res.data?.server_address || '';
        this.loaded = true;
      } catch {
        // Config endpoint unavailable; leave defaults
      }
    },
  },
});
