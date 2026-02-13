<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2024 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https:  www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <div class="space-y-4">
    <div class="table-header-container" v-if="admin">
      <RouterLink to="/admin/peers/new">
        <ShadButton>Enroll New Peer</ShadButton>
      </RouterLink>
    </div>

    <DataTable
      :columns="columns"
      :data="peers"
      :loading="loading"
      :loading-text="'Loading...'"
      :empty-text="'No peers found'"
      :manual-pagination="true"
      :page-index="page - 1"
      :page-size="rows"
      :page-count="totalPages"
      @update:page-index="handlePageIndexUpdate"
      @update:page-size="handlePageSizeUpdate"
    />
  </div>
</template>

<script lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { h } from 'vue';
import { format, formatDistanceToNow } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { buttonVariants } from '@/components/ui/button';
import { DataTable } from '@/components/ui/data-table';

import API from '@/services/API';
import { getWebsocketURI } from '@/services/util';

type PeerRow = {
  id: number;
  ingress: boolean;
  egress: boolean;
  created_at: string | Date;
  last_ping_time: string | Date;
  editable: boolean;
};

export default {
  name: 'RepeaterTable',
  props: {
    admin: Boolean,
  },
  components: {
    ShadButton,
    DataTable,
  },
  data: function() {
    return {
      peers: [] as PeerRow[],
      socket: null as WebSocket | null,
      editablePeers: 0,
      refresh: null as ReturnType<typeof setInterval> | null,
      totalRecords: 0,
      page: 1,
      rows: 30,
      loading: false,
    };
  },
  mounted() {
    this.fetchData();
    if (!this.admin) {
      this.socket = new WebSocket(getWebsocketURI() + '/peers');
      this.mapSocketEvents();
    }
  },
  unmounted() {
    if (this.refresh !== null) {
      clearInterval(this.refresh);
    }
    if (this.socket) {
      this.socket.close();
    }
  },
  computed: {
    columns() {
      return [
        {
          accessorKey: 'id',
          header: 'Peer ID',
          cell: ({ row }: { row: { original: PeerRow } }) => `${row.original.id}`,
        },
        {
          accessorKey: 'last_ping_time',
          header: 'Last Ping',
          cell: ({ row }: { row: { original: PeerRow } }) => {
            const peer = row.original;
            return this.hasTimestamp(peer.last_ping_time) ? this.relativeTime(peer.last_ping_time) : 'Never';
          },
        },
        {
          accessorKey: 'ingress',
          header: 'Ingress',
          cell: ({ row }: { row: { original: PeerRow } }) => {
            const peer = row.original;
            return h('input', {
              id: 'ingress',
              type: 'checkbox',
              checked: peer.ingress,
              disabled: !peer.editable,
            });
          },
        },
        {
          accessorKey: 'egress',
          header: 'Egress',
          cell: ({ row }: { row: { original: PeerRow } }) => {
            const peer = row.original;
            return h('input', {
              id: 'egress',
              type: 'checkbox',
              checked: peer.egress,
              disabled: !peer.editable,
            });
          },
        },
        {
          accessorKey: 'created_at',
          header: 'Created',
          cell: ({ row }: { row: { original: PeerRow } }) => {
            const peer = row.original;
            return h('span', { title: this.absoluteTime(peer.created_at) }, this.relativeTime(peer.created_at));
          },
        },
        {
          accessorKey: 'actions',
          header: 'Actions',
          cell: ({ row }: { row: { original: PeerRow } }) => {
            const peer = row.original;
            if (peer.editable) {
              return '';
            }
            return h('button', {
              class: buttonVariants({ variant: 'outline', size: 'sm' }),
              onClick: () => this.deletePeer(peer),
            }, 'Delete');
          },
        },
      ] as ColumnDef<PeerRow, unknown>[];
    },
    totalPages() {
      if (!this.totalRecords || this.totalRecords <= 0) {
        return 1;
      }
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
  },
  methods: {
    handlePageIndexUpdate(pageIndex: number) {
      const nextPage = pageIndex + 1;
      if (nextPage === this.page || nextPage < 1) {
        return;
      }
      this.page = nextPage;
      this.fetchData(this.page, this.rows);
    },
    handlePageSizeUpdate(pageSize: number) {
      if (pageSize === this.rows || pageSize <= 0) {
        return;
      }
      this.rows = pageSize;
      this.page = 1;
      this.fetchData(this.page, this.rows);
    },
    fetchData(page = 1, limit = 30) {
      this.loading = true;
      if (this.editablePeers === 0) {
        if (this.admin) {
          API.get(`/peers?limit=${limit}&page=${page}`)
            .then((res) => {
              this.peers = this.cleanData(res.data.peers);
              this.totalRecords = res.data.total;
              this.loading = false;
            })
            .catch((err) => {
              console.error(err);
              this.loading = false;
            });
        } else {
          API.get(`/peers/my?limit=${limit}&page=${page}`)
            .then((res) => {
              this.peers = this.cleanData(res.data.peers);
              this.totalRecords = res.data.total;
              this.loading = false;
            })
            .catch((err) => {
              console.error(err);
              this.loading = false;
            });
        }
      }
    },
    cleanData(data: PeerRow[]) {
      const copyData: PeerRow[] = JSON.parse(JSON.stringify(data));
      for (const peer of copyData) {
        peer.created_at = new Date(peer.created_at);
        peer.last_ping_time = new Date(peer.last_ping_time);
        peer.editable = false;
      }
      return copyData;
    },
    hasTimestamp(dateValue: string | Date) {
      const date = new Date(dateValue);
      return !Number.isNaN(date.getTime()) && date.getUTCFullYear() > 1;
    },
    relativeTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return formatDistanceToNow(date, { addSuffix: true });
    },
    absoluteTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return format(date, 'yyyy-MM-dd HH:mm:ss');
    },
    startEdit(peer: Record<string, unknown>) {
      peer.editable = true;
      this.editablePeers++;
    },
    cancelEdit(peer: Record<string, unknown>) {
      peer.editable = false;
      this.editablePeers--;
      if (this.editablePeers == 0) {
        this.fetchData();
      }
    },
    stopEdit() {},
    deletePeer(peer: Record<string, unknown>) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: 'Are you sure you want to delete this peer?' +
          (this.admin ? '':' Only admins may create them.'),
        header: 'Delete Peer',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.delete('/peers/' + peer.id)
            .then(() => {
              this.$toast.add({
                summary: 'Confirmed',
                severity: 'success',
                detail: `Peer ${peer.id} deleted`,
                life: 3000,
              });
              this.fetchData();
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                severity: 'error',
                summary: 'Error',
                detail: `Error deleting peer ${peer.id}`,
                life: 3000,
              });
            });
        },
        reject: () => {},
      });
    },
    mapSocketEvents() {
      const socket = this.socket;
      if (!socket) return;
      socket.addEventListener('open', () => {
        console.log('Connected to peers websocket');
        socket.send('PING');
      });

      socket.addEventListener('error', (event: Event) => {
        console.error('Error from peers websocket', event);
        socket.close();
        this.socket = new WebSocket(getWebsocketURI() + '/peers');
        this.mapSocketEvents();
      });

      socket.addEventListener('message', (event: MessageEvent) => {
        if (event.data == 'PONG') {
          setTimeout(() => {
            if (this.socket) this.socket.send('PING');
          }, 1000);
          return;
        }
      });
    },
  },
};
</script>

<style scoped>
.table-header-container {
  display: flex;
  justify-content: flex-end;
}

.chips:not(:first-child) {
  margin-left: 0.5em;
}

.chips .p-chip {
  margin-top: 0.25em;
}
</style>
