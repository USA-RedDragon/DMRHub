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

export type OAuthScope = {
  name: string;
  label: string;
  description: string;
};

export type OAuthRequest = {
  origin: string;
  scopes: OAuthScope[];
};

export type App = {
  id: string;
  origin: string;
  can_use: string;
  allowed: string;
  last_used: string;
};

export async function getAuthorize(params: Record<string, string>): Promise<OAuthRequest> {
  const res = await API.get('/oauth/authorize', { params });
  return res.data;
}

export async function answerAuthorize(params: Record<string, string>, approve: boolean): Promise<string> {
  const res = await API.post('/oauth/authorize', { ...params, approve });
  return res.data.redirect;
}

export async function getApps(): Promise<App[]> {
  const res = await API.get('/oauth/apps');
  return res.data ?? [];
}

export async function removeApp(id: string): Promise<void> {
  await API.delete(`/oauth/apps/${encodeURIComponent(id)}`);
}
