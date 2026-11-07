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

export interface RepeaterData {
  id: number;
  callsign: string;
  city: string;
  state: string;
  country: string;
  rx_frequency: number;
  tx_frequency: number;
  color_code: number;
  description: string;
  location: string;
}

const cache = new Map<number, RepeaterData | null>();
const inflight = new Map<number, Promise<RepeaterData | null>>();

export function getRepeater(id: number): Promise<RepeaterData | null> {
  if (cache.has(id)) {
    return Promise.resolve(cache.get(id)!);
  }

  if (inflight.has(id)) {
    return inflight.get(id)!;
  }

  const promise = API.get(`/repeaters/${id}`)
    .then((res) => {
      const data: RepeaterData = res.data;
      cache.set(id, data);
      return data;
    })
    .catch(() => {
      cache.set(id, null);
      return null;
    })
    .finally(() => {
      inflight.delete(id);
    });

  inflight.set(id, promise);
  return promise;
}

/** A repeater with no owner, from /repeaters/unassigned. */
export interface UnassignedRepeater {
  id: number;
  type: string;
  callsign: string;
  rx_frequency: number;
  tx_frequency: number;
  color_code: number;
  slots: number;
  connected_time: string;
  created_at: string;
}

export type UnassignedRepeaterList = {
  total: number;
  repeaters: UnassignedRepeater[];
};

export async function getUnassignedRepeaters(page: number, limit: number): Promise<UnassignedRepeaterList> {
  const res = await API.get(`/repeaters/unassigned?page=${page}&limit=${limit}`);
  return { total: res.data?.total ?? 0, repeaters: res.data?.repeaters ?? [] };
}

export async function assignRepeaterOwner(id: number, owner: number): Promise<void> {
  await API.post(`/repeaters/${id}/owner`, { owner });
}

const mhz = (hz: number) => (hz / 1e6).toFixed(4);

/** The frequencies of r in MHz: one for a simplex channel, RX and TX for a duplex one. */
export function frequencyLabel(r: Pick<UnassignedRepeater, 'rx_frequency' | 'tx_frequency'>): string {
  if (!r.rx_frequency && !r.tx_frequency) {
    return '';
  }
  if (r.rx_frequency === r.tx_frequency || !r.tx_frequency || !r.rx_frequency) {
    return `${mhz(r.rx_frequency || r.tx_frequency)} MHz`;
  }
  return `RX ${mhz(r.rx_frequency)} / TX ${mhz(r.tx_frequency)} MHz`;
}

const kinds: Record<string, string> = { modem: 'Modem', mmdvm: 'MMDVM', ipsc: 'IPSC' };

/** The kind of r: its type and, for a modem, hotspot or repeater from its slots. */
export function kindLabel(r: Pick<UnassignedRepeater, 'type' | 'slots'>): string {
  const kind = kinds[r.type] ?? r.type.toUpperCase();
  if (r.type !== 'modem') {
    return kind;
  }
  return r.slots === 4 ? `${kind}, hotspot` : `${kind}, repeater`;
}

/** SESSION_TIMEOUT_MS is how long after its last ping the hub keeps a repeater logged in. */
export const SESSION_TIMEOUT_MS = 60_000;

function timeOf(s: string | null | undefined): number {
  if (!s) return 0;
  const t = new Date(s).getTime();
  return Number.isNaN(t) || new Date(s).getFullYear() <= 1 ? 0 : t;
}

/**
 * repeaterOnline reports whether a repeater has a session at now, as the
 * server's Repeater.Online decides it: it pinged within SESSION_TIMEOUT_MS,
 * and no session ended at or after that ping.
 */
export function repeaterOnline(r: { last_ping_time?: string; disconnected_time?: string | null }, now: number): boolean {
  const ping = timeOf(r.last_ping_time);
  if (!ping || now - ping >= SESSION_TIMEOUT_MS) return false;
  const down = timeOf(r.disconnected_time);
  return !down || ping > down;
}
