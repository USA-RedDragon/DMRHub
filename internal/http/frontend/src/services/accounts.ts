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

export type Invite = {
  id: number;
  created_by: number;
  created_by_callsign: string;
  created_at: string;
  expires_at: string;
  used_by: number | null;
  used_by_callsign?: string;
  used_at: string | null;
};

export type NewInvite = Invite & { code: string; link: string };

export type MessageAccessRadioID = { radio_id: number; user_id: number | null; callsign?: string };

export type MessageAccess = {
  id: number;
  at: string;
  admin_id: number;
  admin_callsign: string;
  kind: 'search' | 'read';
  message_id: number | null;
  filter: string;
  results: number;
  radio_ids: MessageAccessRadioID[];
};

export type MessageAccessQuery = { radioID?: number; admin?: number; page: number; limit: number };

export async function getInvites(): Promise<Invite[]> {
  const res = await API.get('/admin/invites');
  return res.data.invites ?? [];
}

export async function createInvite(): Promise<NewInvite> {
  const res = await API.post('/admin/invites');
  return res.data;
}

export async function deleteInvite(id: number): Promise<void> {
  await API.delete(`/admin/invites/${id}`);
}

export async function getMessageAccesses(q: MessageAccessQuery): Promise<{ total: number; accesses: MessageAccess[] }> {
  const params = new URLSearchParams({ page: String(q.page), limit: String(q.limit) });
  if (q.radioID) {
    params.set('radio_id', String(q.radioID));
  }
  if (q.admin) {
    params.set('admin', String(q.admin));
  }
  const res = await API.get(`/admin/message-access?${params.toString()}`);
  return { total: res.data.total ?? 0, accesses: res.data.accesses ?? [] };
}

/** The search of an access log entry as the admin typed it, such as "from 3191868". */
export function accessWhat(a: MessageAccess): string {
  if (a.kind === 'read') {
    return `Message ${a.message_id ?? ''}`.trim();
  }
  const p = new URLSearchParams(a.filter);
  const parts: string[] = [];
  const names: [string, string][] = [['src', 'from'], ['dst', 'to'], ['format', 'format'], ['text', 'text']];
  for (const [key, name] of names) {
    const v = p.get(key);
    if (v) {
      parts.push(key === 'text' ? `${name} "${v}"` : `${name} ${v}`);
    }
  }
  return parts.length > 0 ? `Search: ${parts.join(', ')}` : 'Search';
}
