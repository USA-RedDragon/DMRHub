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

export type Role = 'owner' | 'cosysop' | 'admin';

export type Cosysop = {
  id: number;
  callsign: string;
  username: string;
  invited_at: string;
  accepted_at: string | null;
  expires_at: string | null;
  pending: boolean;
};

export type Cosysops = {
  owner: { id: number; callsign: string } | null;
  role: Role;
  cosysops: Cosysop[];
};

export type CosysopInvite = {
  repeater_id: number;
  repeater_callsign: string;
  owner: { id: number; callsign: string } | null;
  invited_at: string;
  expires_at: string;
};

/** canManage reports whether role may change the talkgroups, rules and codec of a repeater. */
export function canManage(role: Role | null): boolean {
  return role === 'owner' || role === 'cosysop' || role === 'admin';
}

/** asCosysops returns data as Cosysops, or null when it is not a list of co-sysops. */
export function asCosysops(data: unknown): Cosysops | null {
  const d = data as Partial<Cosysops> | null;
  if (!d || !canManage(d.role ?? null) || !Array.isArray(d.cosysops)) return null;
  return d as Cosysops;
}

/** canSeeSecret reports whether role may see or rotate the password of a repeater. */
export function canSeeSecret(role: Role | null): boolean {
  return role === 'owner' || role === 'admin';
}
