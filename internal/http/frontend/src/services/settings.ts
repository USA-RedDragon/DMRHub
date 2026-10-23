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

export type UserSettings = {
  sms_storage: boolean;
  share_positions?: boolean;
};

export type RadioID = {
  id: number;
  callsign: string;
  account: boolean;
  verified_at: string | null;
};

export async function getSettings(): Promise<UserSettings> {
  const res = await API.get('/users/me/settings');
  return res.data;
}

export async function setSMSStorage(enabled: boolean): Promise<UserSettings> {
  const res = await API.put('/users/me/settings/sms-storage', { enabled });
  return res.data;
}

export async function getRadioIDs(): Promise<RadioID[]> {
  const res = await API.get('/users/me/radio-ids');
  return res.data.radio_ids ?? [];
}

export async function addRadioID(id: number): Promise<RadioID> {
  const res = await API.post('/users/me/radio-ids', { id });
  return res.data;
}

export async function removeRadioID(id: number): Promise<void> {
  await API.delete(`/users/me/radio-ids/${id}`);
}

export function errorText(err: unknown, fallback: string): string {
  const data = (err as { response?: { data?: { error?: unknown } } })?.response?.data;
  if (data && typeof data.error === 'string' && data.error !== '') {
    return data.error;
  }
  return fallback;
}

export function errorStatus(err: unknown): number {
  return (err as { response?: { status?: number } })?.response?.status ?? 0;
}

export async function setSharePositions(enabled: boolean): Promise<UserSettings> {
  const res = await API.put('/users/me/settings/share-positions', { enabled });
  return res.data;
}
