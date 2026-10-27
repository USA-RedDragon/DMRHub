<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2026 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https://www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <Card data-test="replicas">
    <CardHeader>
      <CardTitle>Replicas</CardTitle>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <p>Each replica reports the last save it put into effect.</p>
      <ShadTable>
        <TableHeader>
          <TableRow>
            <TableHead>Replica</TableHead>
            <TableHead>Save</TableHead>
            <TableHead>Reported</TableHead>
            <TableHead>State</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="r in replicas" :key="r.id" :data-test="`replica-${r.id}`">
            <TableCell class="font-mono">{{ name(r.id) }}<template v-if="r.self"> (this one)</template></TableCell>
            <TableCell class="tabular-nums">#{{ r.revision }}, {{ r.current ? 'latest' : 'behind' }}</TableCell>
            <TableCell class="tabular-nums">{{ reported(r.at) }}</TableCell>
            <TableCell>
              <span class="mr-1.5 inline-block size-2 rounded-full align-[1px]" :class="tone(r)" />
              {{ state(r) }}
            </TableCell>
          </TableRow>
        </TableBody>
      </ShadTable>
    </CardContent>
  </Card>
</template>

<script lang="ts">
import { defineComponent, type PropType } from 'vue';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table as ShadTable, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { replicaName, serverName, settingLabel, type ReplicaStatus } from '@/lib/setupState';

export default defineComponent({
  components: { Card, CardContent, CardHeader, CardTitle, ShadTable, TableBody, TableCell, TableHead, TableHeader, TableRow },
  props: {
    replicas: { type: Array as PropType<ReplicaStatus[]>, required: true },
    revision: { type: Number, required: true },
  },
  methods: {
    name: replicaName,
    reported(at: string): string {
      const d = new Date(at);
      return Number.isNaN(d.getTime()) ? 'never' : d.toLocaleTimeString([], { timeZoneName: 'short' });
    },
    failed(r: ReplicaStatus): boolean {
      return (r.apply_errors?.length ?? 0) > 0 || (r.invalid?.length ?? 0) > 0;
    },
    tone(r: ReplicaStatus): string {
      if (this.failed(r)) {
        return 'bg-destructive';
      }
      return r.current ? 'bg-green-600 dark:bg-green-400' : 'bg-amber-500';
    },
    state(r: ReplicaStatus): string {
      const invalid = r.invalid ?? [];
      if (invalid.length > 0) {
        return `Cannot run with the saved ${invalid.map((e) => settingLabel(e.field)).join(', ')}; keeps its previous settings`;
      }
      const errs = r.apply_errors ?? [];
      if (errs.length > 0) {
        return errs.map((e) => `${serverName(e.server)} server ${e.running ? 'kept its previous settings' : 'is not running'}`).join('; ');
      }
      return r.current ? 'All settings in effect' : 'Has not taken the latest save yet';
    },
  },
});
</script>
