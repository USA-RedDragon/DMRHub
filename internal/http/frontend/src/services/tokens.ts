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
import type { OAuthScope } from '@/services/oauth';

export type PersonalToken = {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  full: boolean;
  can_use: string;
  days: number;
  made: string;
  last_used: string | null;
  expires: string | null;
  expired: boolean;
};

export type PersonalTokens = {
  tokens: PersonalToken[];
  scopes: OAuthScope[];
  expiries: number[];
};

export type NewPersonalToken = {
  name: string;
  scopes: string[];
  full: boolean;
  expires_in_days: number;
};

export type MadeToken = {
  token: PersonalToken;
  secret: string;
};

export async function getTokens(): Promise<PersonalTokens> {
  const res = await API.get('/users/me/tokens');
  return {
    tokens: res.data?.tokens ?? [],
    scopes: res.data?.scopes ?? [],
    expiries: res.data?.expiries ?? [],
  };
}

export async function makeToken(body: NewPersonalToken): Promise<MadeToken> {
  const res = await API.post('/users/me/tokens', body);
  return res.data;
}

export async function rotateToken(id: string): Promise<MadeToken> {
  const res = await API.post(`/users/me/tokens/${encodeURIComponent(id)}/rotate`);
  return res.data;
}

export async function deleteToken(id: string): Promise<void> {
  await API.delete(`/users/me/tokens/${encodeURIComponent(id)}`);
}
