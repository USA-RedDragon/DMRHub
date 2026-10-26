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
import { shortTime } from '@/lib/codec';

export type PluginRange = { first: number; last: number };

export type PluginKind = 'external' | 'builtin' | 'uploaded';

export type PluginHost = { address: string; reason: string; allowed: boolean };

export type AdminPlugin = {
  id: number;
  name: string;
  kind: PluginKind;
  version: string;
  author: string;
  description: string;
  enabled: boolean;
  ranges: PluginRange[];
  hosts: PluginHost[];
  online: boolean;
  connected_since: string | null;
  last_seen_at: string | null;
  address: string;
  token_created_at: string;
  calls_today: number;
  state?: PluginState;
  stopped_at?: string;
  restart_at?: string;
  stop_reason?: string;
  stops_last_hour?: number;
  waiting_id?: number;
  waiting_for?: string;
};

export type PluginState = 'off' | 'running' | 'starting' | 'stopped' | 'waiting';

export type PluginConnection = {
  enabled: boolean;
  address: string;
  fingerprint: string;
  own_certificate: boolean;
};

export type PluginList = { plugins: AdminPlugin[]; connection: PluginConnection; inProcess: boolean };

export type PluginManifest = {
  name: string;
  version: string;
  author: string;
  description: string;
  ranges: PluginRange[];
  hosts: PluginHost[];
};

export type PluginToken = { plugin: AdminPlugin; token: string };


export type PluginInput = { name: string; enabled: boolean; ranges: PluginRange[]; allowed_hosts?: string[] };

export async function listPlugins(): Promise<PluginList> {
  const res = await API.get('/admin/plugins');
  const plugins: AdminPlugin[] = (res.data.plugins ?? []).map((p: AdminPlugin) => ({ ...p, hosts: p.hosts ?? [] }));
  return { plugins, connection: res.data.connection, inProcess: res.data.in_process === true };
}

function fileForm(file: File): FormData {
  const form = new FormData();
  form.append('file', file);
  return form;
}

/** inspectPlugin returns what the plugin file says it is and asks for, and adds nothing. */
export async function inspectPlugin(file: File): Promise<PluginManifest> {
  const res = await API.post('/admin/plugins/inspect', fileForm(file));
  return { ...res.data, ranges: res.data.ranges ?? [], hosts: res.data.hosts ?? [] };
}

/** uploadPlugin adds the plugin file with its IDs and the hosts it may reach, and starts it. */
export async function uploadPlugin(file: File, ranges: PluginRange[], allowedHosts: string[]): Promise<AdminPlugin> {
  const form = fileForm(file);
  form.append('ranges', JSON.stringify(ranges));
  form.append('allowed_hosts', JSON.stringify(allowedHosts));
  const res = await API.post('/admin/plugins/upload', form);
  return res.data;
}

/** uploadPluginVersion replaces the module of an uploaded plugin with a newer file. */
export async function uploadPluginVersion(id: number, file: File): Promise<AdminPlugin> {
  const res = await API.post(`/admin/plugins/${id}/module`, fileForm(file));
  return res.data;
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

/** isInside reports whether p runs inside DMRHub. */
export function isInside(p: AdminPlugin): boolean {
  return p.kind === 'builtin' || p.kind === 'uploaded';
}

/** runsLabel says where a plugin of kind runs. */
export function runsLabel(kind: PluginKind): string {
  switch (kind) {
    case 'builtin':
      return 'Built in';
    case 'uploaded':
      return 'Uploaded';
    default:
      return 'External';
  }
}

/** formatIDs is the IDs of a plugin as its IDs field shows them: 9990, 5050-5059. */
export function formatIDs(ranges: PluginRange[]): string {
  return ranges.map((r) => (r.first === r.last ? `${r.first}` : `${r.first}-${r.last}`)).join(', ');
}

/** parseIDs reads IDs and ranges such as 9990, 5050-5059, and returns null for anything else. */
export function parseIDs(text: string): PluginRange[] | null {
  if (text.trim() === '') {
    return [];
  }
  const out: PluginRange[] = [];
  for (const part of text.split(',')) {
    const m = /^\s*(\d+)\s*(?:-\s*(\d+)\s*)?$/.exec(part);
    if (!m) {
      return null;
    }
    const first = Number(m[1]);
    const last = m[2] === undefined ? first : Number(m[2]);
    if (last < first) {
      return null;
    }
    out.push({ first, last });
  }
  return out;
}

/** secondsUntil is the whole seconds from now until iso, or 0 once it passed. */
export function secondsUntil(iso: string | undefined, now: number = Date.now()): number {
  if (!iso) {
    return 0;
  }
  return Math.max(0, Math.ceil((Date.parse(iso) - now) / 1000));
}

/** insideState is the state of a plugin inside DMRHub at now: stopped turns to starting once its wait is over. */
export function insideState(p: AdminPlugin, now: number = Date.now()): PluginState {
  if (!p.enabled || p.state === 'off') {
    return 'off';
  }
  if (p.state === 'stopped' && secondsUntil(p.restart_at, now) === 0) {
    return 'starting';
  }
  if (p.state) {
    return p.state;
  }
  return p.online ? 'running' : 'starting';
}

/** status is what the list says of a plugin's connection, or of a plugin inside DMRHub. */
export function status(p: AdminPlugin, now: number = Date.now(), timeZone?: string): string {
  if (!p.enabled) {
    return 'Off';
  }
  if (isInside(p)) {
    switch (insideState(p, now)) {
      case 'off':
        return 'Off';
      case 'running':
        return 'Running in DMRHub';
      case 'waiting':
        return `Waiting for its IDs, ${p.waiting_id} is used by ${p.waiting_for}`;
      case 'stopped':
        return `Stopped at ${shortTime(new Date(p.stopped_at ?? ''), timeZone)}, starts again in ${secondsUntil(p.restart_at, now)} s`;
      default:
        return 'Starting';
    }
  }
  if (p.online && p.connected_since) {
    return `Connected for ${formatSpan(p.connected_since, now)}, from ${p.address.replace(/:\d+$/, '')}`;
  }
  if (p.last_seen_at) {
    return `Not connected, last seen ${formatSpan(p.last_seen_at, now)} ago`;
  }
  return 'Not connected yet';
}

/** stopNote is the edit page's box about a plugin inside DMRHub that stopped, or null when it did not. */
export function stopNote(p: AdminPlugin, now: number = Date.now(), timeZone?: string): { title: string; body: string } | null {
  if (!isInside(p) || insideState(p, now) !== 'stopped') {
    return null;
  }
  const stops = p.stops_last_hour ?? 1;
  const times = stops === 1 ? 'once' : `${stops} times`;
  return {
    title: `Stopped at ${shortTime(new Date(p.stopped_at ?? ''), timeZone)}.`,
    body: `${p.stop_reason ?? ''} DMRHub starts it again in ${secondsUntil(p.restart_at, now)} s. It has stopped ${times} in the last hour.`,
  };
}

/** statusTone is the color of a plugin's status dot. */
export function statusTone(p: AdminPlugin, now: number = Date.now()): 'ok' | 'info' | 'bad' | 'warn' | 'none' {
  if (!p.enabled) {
    return 'none';
  }
  if (!isInside(p)) {
    return p.online ? 'ok' : 'none';
  }
  switch (insideState(p, now)) {
    case 'running':
      return 'ok';
    case 'starting':
      return 'info';
    case 'stopped':
      return 'bad';
    case 'waiting':
      return 'warn';
    default:
      return 'none';
  }
}

/** formatDay is a date in words: 23 October 2026. */
export function formatDay(iso: string): string {
  return new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
}
