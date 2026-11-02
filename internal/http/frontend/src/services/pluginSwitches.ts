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


import API from '@/services/API';

export type PluginSwitchDetail = {
  label: string;
  value: string;
};

export type PluginSwitchChoice = {
  label: string;
  options: string[];
  value: string;
};

export type PluginSwitch = {
  plugin_id: number;
  plugin: string;
  key: string;
  title: string;
  description: string;
  notice: string;
  details: PluginSwitchDetail[];
  choice: PluginSwitchChoice | null;
  on: boolean;
};

export async function getPluginSwitches(): Promise<PluginSwitch[]> {
  const res = await API.get('/users/me/plugin-switches');
  return Array.isArray(res.data) ? res.data : [];
}

export async function setPluginSwitch(pluginID: number, key: string, on: boolean, choice: string): Promise<PluginSwitch> {
  const res = await API.put(`/users/me/plugin-switches/${pluginID}/${encodeURIComponent(key)}`, { on, choice });
  return res.data;
}

// codeParts splits a detail value into plain text and the parts between
// backticks, which show in a fixed-width font.
export function codeParts(value: string): { text: string; code: boolean }[] {
  return value
    .split('`')
    .map((text, i) => ({ text, code: i % 2 === 1 }))
    .filter((p) => p.text !== '');
}
