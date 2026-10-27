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

/** The servers that the page of an OpenBridge peer has steps for. */
export type PeerTab = 'freedmr' | 'hblink3' | 'other';

/** The name of each server tab, and whether its steps were tested: null for a tab that has no test mark. */
export const PEER_TABS: Record<PeerTab, { name: string; tested: boolean | null }> = {
  freedmr: { name: 'FreeDMR', tested: true },
  hblink3: { name: 'HBlink3', tested: true },
  other: { name: 'Other OpenBridge server', tested: null },
};

/** Returns the server tabs of a peer: FreeDMR for an enhanced peer, else HBlink3 and any other server. */
export function peerTabs(enhanced: boolean): PeerTab[] {
  return enhanced ? ['freedmr'] : ['hblink3', 'other'];
}

/** The values that the server steps fill in. */
export type PeerValues = {
  host: string;
  port: number;
  id: number;
  password: string;
  ip: string;
  peerPort: string;
};

/** PEER_PASSWORD_PLACEHOLDER stands in for the password when the page does not show it. */
export const PEER_PASSWORD_PLACEHOLDER = '<peer password>';

const passwords = new Map<number, string>();

/** Keeps the password of the peer id for the next takePeerPassword, as the new peer page hands it to the peer page. */
export function keepPeerPassword(id: number, password: string): void {
  passwords.set(id, password);
}

/** Returns the password that keepPeerPassword kept for the peer id once, and forgets it. */
export function takePeerPassword(id: number): string | undefined {
  const p = passwords.get(id);
  passwords.delete(id);
  return p;
}
