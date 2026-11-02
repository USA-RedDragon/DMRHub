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

export type OnAirUnlock = {
  radio: number;
  repeater: number;
  callsign: string;
  slot: number;
  until: string;
};

export type OnAirCode = {
  on: boolean;
  unlock_seconds: number;
  unlocks: OnAirUnlock[];
};

export type OnAirSetup = {
  key: string;
  uri: string;
  qr: string;
};

function state(data: Partial<OnAirCode> | undefined): OnAirCode {
  return { on: data?.on === true, unlock_seconds: data?.unlock_seconds ?? 0, unlocks: data?.unlocks ?? [] };
}

export async function getOnAirCode(): Promise<OnAirCode> {
  const res = await API.get('/users/me/totp');
  return state(res.data);
}

export async function startOnAirSetup(): Promise<OnAirSetup> {
  const res = await API.post('/users/me/totp/setup');
  return res.data;
}

export async function turnOnAirCodeOn(code: string): Promise<OnAirCode> {
  const res = await API.post('/users/me/totp', { code });
  return state(res.data);
}

export async function turnOnAirCodeOff(code: string): Promise<OnAirCode> {
  const res = await API.delete('/users/me/totp', { data: { code } });
  return state(res.data);
}

export async function lockRadio(u: OnAirUnlock): Promise<OnAirCode> {
  const res = await API.delete(`/users/me/totp/unlocks/${u.radio}/${u.repeater}/${u.slot}`);
  return state(res.data);
}

export async function resetOnAirCode(userID: number): Promise<void> {
  await API.delete(`/users/${userID}/totp`);
}
