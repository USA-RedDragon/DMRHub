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

import type { ColumnDef } from '@tanstack/vue-table';
import { h } from 'vue';
import User from '@/components/User.vue';
import Talkgroup from '@/components/Talkgroup.vue';
import Repeater from '@/components/Repeater.vue';
import RelativeTimestamp from '@/components/RelativeTimestamp.vue';
import TransmissionType from '@/components/TransmissionType.vue';
import {
  berText,
  durationText,
  jitterText,
  lossText,
  rssiText,
  type CallRow,
} from '@/lib/transmissions';

type Cell = { row: { original: CallRow } };

export type LastHeardColumnOptions = {
  user?: boolean;
  destination?: boolean;
  repeater?: boolean;
};

function person(u: { id: number; callsign: string }) {
  if (!u.callsign) {
    return h('span', { class: 'font-medium' }, String(u.id));
  }
  return h(User, { user: u });
}

function destination(call: CallRow) {
  if (call.is_to_talkgroup) {
    return h(Talkgroup, { talkgroup: call.to_talkgroup });
  }
  if (call.is_to_repeater) {
    return `${call.to_repeater.callsign} | ${call.to_repeater.id}`;
  }
  if (call.is_to_user) {
    return h(User, { user: call.to_user });
  }
  if (!call.destination_id) {
    return '-';
  }
  return h('span', { class: 'font-medium' }, call.group_call ? `TG ${call.destination_id}` : String(call.destination_id));
}

export function lastHeardColumns(opts: LastHeardColumnOptions = {}): ColumnDef<CallRow, unknown>[] {
  const { user = true, destination: showDestination = true, repeater = false } = opts;
  const columns: ColumnDef<CallRow, unknown>[] = [
    {
      accessorKey: 'time',
      header: 'Time',
      cell: ({ row }: Cell) => {
        const call = row.original;
        if (call.active) {
          return 'Active';
        }
        return h(RelativeTimestamp, { time: call.start_time, active: call.active });
      },
    },
    {
      accessorKey: 'time_slot',
      header: 'TS',
      cell: ({ row }: Cell) => (row.original.time_slot ? '2' : '1'),
    },
    {
      accessorKey: 'kind',
      header: 'Type',
      cell: ({ row }: Cell) => h(TransmissionType, { call: row.original }),
    },
  ];
  if (user) {
    columns.push({
      accessorKey: 'user',
      header: 'User',
      cell: ({ row }: Cell) => person(row.original.user),
    });
  }
  if (showDestination) {
    columns.push({
      accessorKey: 'destination',
      header: 'Destination',
      cell: ({ row }: Cell) => destination(row.original),
    });
  }
  if (repeater) {
    columns.push({
      accessorKey: 'repeater',
      header: 'Repeater',
      cell: ({ row }: Cell) => {
        const r = row.original.repeater;
        if (!r || !r.id) {
          return '-';
        }
        return h(Repeater, { repeater: r });
      },
    });
  }
  columns.push(
    {
      accessorKey: 'duration',
      header: 'Duration',
      cell: ({ row }: Cell) => durationText(row.original),
    },
    {
      accessorKey: 'ber',
      header: 'BER',
      cell: ({ row }: Cell) => berText(row.original),
    },
    {
      accessorKey: 'loss',
      header: 'Loss',
      cell: ({ row }: Cell) => lossText(row.original),
    },
    {
      accessorKey: 'jitter',
      header: 'Jitter',
      cell: ({ row }: Cell) => jitterText(row.original),
    },
    {
      accessorKey: 'rssi',
      header: 'RSSI',
      cell: ({ row }: Cell) => rssiText(row.original),
    },
  );
  return columns;
}
