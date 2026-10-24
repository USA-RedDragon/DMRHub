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

export type PluginRange = { first: number; last: number };

export type AdminPlugin = {
  id: number;
  name: string;
  enabled: boolean;
  ranges: PluginRange[];
  online: boolean;
  connected_since: string | null;
  last_seen_at: string | null;
  address: string;
  token_created_at: string;
  calls_today: number;
};

export type PluginConnection = {
  enabled: boolean;
  address: string;
  fingerprint: string;
  own_certificate: boolean;
};

export type PluginList = { plugins: AdminPlugin[]; connection: PluginConnection };

export type PluginToken = { plugin: AdminPlugin; token: string };

export type PluginInput = { name: string; enabled: boolean; ranges: PluginRange[] };

export async function listPlugins(): Promise<PluginList> {
  const res = await API.get('/admin/plugins');
  return { plugins: res.data.plugins ?? [], connection: res.data.connection };
}

export async function addPlugin(input: PluginInput): Promise<PluginToken> {
  const res = await API.post('/admin/plugins', { name: input.name, ranges: input.ranges });
  return res.data;
}

export async function updatePlugin(id: number, input: PluginInput): Promise<AdminPlugin> {
  const res = await API.put(`/admin/plugins/${id}`, input);
  return res.data;
}

export async function newPluginToken(id: number): Promise<PluginToken> {
  const res = await API.post(`/admin/plugins/${id}/token`);
  return res.data;
}

export async function deletePlugin(id: number): Promise<void> {
  await API.delete(`/admin/plugins/${id}`);
}

/** formatRanges is the IDs of a plugin as the list shows them: 310001–310009, 9998. */
export function formatRanges(ranges: PluginRange[]): string {
  if (ranges.length === 0) {
    return 'None';
  }
  return ranges.map((r) => (r.first === r.last ? `${r.first}` : `${r.first}–${r.last}`)).join(', ');
}

/** formatSpan is a length of time in words: 2 hours, 3 days, 5 minutes. */
export function formatSpan(fromIso: string, now: number = Date.now()): string {
  const minutes = Math.max(1, Math.round((now - Date.parse(fromIso)) / 60000));
  if (minutes < 60) {
    return minutes === 1 ? '1 minute' : `${minutes} minutes`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 48) {
    return hours === 1 ? '1 hour' : `${hours} hours`;
  }
  return `${Math.round(hours / 24)} days`;
}

/** status is what the list says of a plugin's connection. */
export function status(p: AdminPlugin, now: number = Date.now()): string {
  if (!p.enabled) {
    return 'Off';
  }
  if (p.online && p.connected_since) {
    return `Connected for ${formatSpan(p.connected_since, now)}, from ${p.address.replace(/:\d+$/, '')}`;
  }
  if (p.last_seen_at) {
    return `Not connected, last seen ${formatSpan(p.last_seen_at, now)} ago`;
  }
  return 'Not connected yet';
}

/** formatDay is a date in words: 23 October 2026. */
export function formatDay(iso: string): string {
  return new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
}
