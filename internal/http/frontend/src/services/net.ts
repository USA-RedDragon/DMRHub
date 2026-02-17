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

export type NetTalkgroup = {
  id: number;
  name: string;
  description: string;
};

export type NetUser = {
  id: number;
  callsign: string;
};

export type Net = {
  id: number;
  talkgroup_id: number;
  talkgroup: NetTalkgroup;
  started_by_user: NetUser;
  scheduled_net_id?: number;
  start_time: string;
  end_time?: string;
  duration_minutes?: number;
  description: string;
  active: boolean;
  showcase: boolean;
  check_in_count: number;
};

export type NetCheckIn = {
  call_id: number;
  user: NetUser;
  start_time: string;
  duration: number;
  time_slot: boolean;
  loss: number;
  jitter: number;
  ber: number;
  rssi: number;
};

export type ScheduledNet = {
  id: number;
  talkgroup_id: number;
  talkgroup: NetTalkgroup;
  created_by_user: NetUser;
  name: string;
  description: string;
  cron_expression: string;
  day_of_week: number;
  time_of_day: string;
  timezone: string;
  duration_minutes?: number;
  enabled: boolean;
  next_run?: string;
  created_at: string;
};

export function getNets(params?: {
  page?: number;
  limit?: number;
  talkgroup_id?: number;
  active?: boolean;
  showcase?: boolean;
}) {
  const query = new URLSearchParams();
  if (params?.page) query.set('page', String(params.page));
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.talkgroup_id) query.set('talkgroup_id', String(params.talkgroup_id));
  if (params?.active !== undefined) query.set('active', String(params.active));
  if (params?.showcase !== undefined) query.set('showcase', String(params.showcase));
  const qs = query.toString();
  return API.get<{ nets: Net[]; total: number }>(`/nets${qs ? '?' + qs : ''}`);
}

export function getNet(id: number) {
  return API.get<Net>(`/nets/${id}`);
}

export function startNet(body: {
  talkgroup_id: number;
  description?: string;
  duration_minutes?: number;
}) {
  return API.post<Net>('/nets/start', body);
}

export function stopNet(id: number) {
  return API.post<Net>(`/nets/${id}/stop`);
}

export function patchNet(id: number, body: { showcase?: boolean }) {
  return API.patch<Net>(`/nets/${id}`, body);
}

export function getNetCheckIns(
  id: number,
  params?: { page?: number; limit?: number },
) {
  const query = new URLSearchParams();
  if (params?.page) query.set('page', String(params.page));
  if (params?.limit) query.set('limit', String(params.limit));
  const qs = query.toString();
  return API.get<{ check_ins: NetCheckIn[]; total: number }>(
    `/nets/${id}/checkins${qs ? '?' + qs : ''}`,
  );
}

export function exportNetCheckIns(id: number, format: 'csv' | 'json' = 'csv') {
  return API.get(`/nets/${id}/checkins/export?format=${format}`, {
    responseType: format === 'csv' ? 'blob' : 'json',
  });
}

export function getScheduledNets(params?: {
  page?: number;
  limit?: number;
  talkgroup_id?: number;
}) {
  const query = new URLSearchParams();
  if (params?.page) query.set('page', String(params.page));
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.talkgroup_id)
    query.set('talkgroup_id', String(params.talkgroup_id));
  const qs = query.toString();
  return API.get<{ scheduled_nets: ScheduledNet[]; total: number }>(
    `/nets/scheduled${qs ? '?' + qs : ''}`,
  );
}

export function getScheduledNet(id: number) {
  return API.get<ScheduledNet>(`/nets/scheduled/${id}`);
}

export function createScheduledNet(body: {
  talkgroup_id: number;
  name: string;
  description?: string;
  day_of_week: number;
  time_of_day: string;
  timezone: string;
  duration_minutes?: number;
  enabled?: boolean;
}) {
  return API.post<ScheduledNet>('/nets/scheduled', body);
}

export function updateScheduledNet(
  id: number,
  body: {
    name?: string;
    description?: string;
    day_of_week?: number;
    time_of_day?: string;
    timezone?: string;
    duration_minutes?: number;
    enabled?: boolean;
  },
) {
  return API.patch<ScheduledNet>(`/nets/scheduled/${id}`, body);
}

export function deleteScheduledNet(id: number) {
  return API.delete(`/nets/scheduled/${id}`);
}
