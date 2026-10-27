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

export type RadioIDDrift = {
  radio_id: number;
  user_id: number;
  username: string;
  callsign: string;
  radio_id_callsign: string;
  found_at: string;
  checked_at: string;
  can_fix: boolean;
  can_notify: boolean;
};

export type RadioIDDriftList = {
  total: number;
  drifts: RadioIDDrift[];
};

export type AccountPatch = {
  username?: string;
  callsign?: string;
  override_radio_id?: boolean;
};

export async function getRadioIDDrifts(page: number, limit: number): Promise<RadioIDDriftList> {
  const res = await API.get(`/admin/radio-id-drift?page=${page}&limit=${limit}`);
  return { total: res.data.total ?? 0, drifts: res.data.drifts ?? [] };
}

export async function fixRadioIDDrift(radioID: number, notify: boolean): Promise<{ callsign: string; notified: boolean }> {
  const res = await API.post(`/admin/radio-id-drift/${radioID}/fix`, { notify });
  return res.data;
}

export async function radioIDCallsign(id: number): Promise<string> {
  try {
    const res = await API.get(`/userdb/${id}`);
    return String(res.data?.callsign ?? '').toUpperCase();
  } catch {
    return '';
  }
}

export async function patchAccount(id: number, patch: AccountPatch): Promise<void> {
  await API.patch(`/users/${id}`, patch);
}

export function radioIDMismatch(err: unknown): string | null {
  const data = (err as { response?: { data?: { radio_id_mismatch?: unknown; radio_id_callsign?: unknown } } })?.response?.data;
  if (!data || data.radio_id_mismatch !== true) {
    return null;
  }
  return typeof data.radio_id_callsign === 'string' ? data.radio_id_callsign : '';
}
