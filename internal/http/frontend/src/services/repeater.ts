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
