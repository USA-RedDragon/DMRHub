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

export interface RadioIdData {
  id: number;
  callsign: string;
  name: string;
  surname: string;
  city: string;
  state: string;
  country: string;
  flag: string;
}

const cache = new Map<number, RadioIdData | null>();
const inflight = new Map<number, Promise<RadioIdData | null>>();

export function getUserDB(id: number): Promise<RadioIdData | null> {
  if (cache.has(id)) {
    return Promise.resolve(cache.get(id)!);
  }

  if (inflight.has(id)) {
    return inflight.get(id)!;
  }

  const promise = API.get(`/userdb/${id}`)
    .then((res) => {
      const data: RadioIdData = res.data;
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
