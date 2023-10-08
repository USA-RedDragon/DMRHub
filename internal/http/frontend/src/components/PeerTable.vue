<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023 Jacob McSwain

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
  <DataTable
    :value="peers"
    v-model:expandedRows="expandedRows"
    dataKey="id"
    :lazy="true"
    :first="first"
    :paginator="true"
    :rows="10"
    :totalRecords="totalRecords"
    :loading="loading"
    :scrollable="true"
    @page="onPage($event)"
  >
    <template #header v-if="!this.$props.admin">
      <div class="table-header-container">
        <RouterLink to="/repeaters/peers/new">
          <PVButton
            class="p-button-raised p-button-rounded p-button-success"
            icon="pi pi-plus"
            label="Enroll New Peer"
          />
        </RouterLink>
      </div>
    </template>
    <Column :expander="true" />
    <Column field="id" header="Peer ID"></Column>
    <Column field="last_ping_time" header="Last Ping">
      <template #body="slotProps">
        <span v-if="slotProps.data.last_ping_time.year() != 0">
          {{ slotProps.data.last_ping_time.fromNow() }}
        </span>
        <span v-else>Never</span>
      </template>
    </Column>
    <Column field="ingress" header="Ingress">
      <template #body="slotProps">
        <Checkbox
          id="ingress"
          inputId="ingress"
          v-model="slotProps.data.ingress"
          :binary="true"
          :disabled="!slotProps.data.editable"
        />
      </template>
    </Column>
    <Column field="egress" header="Egress">
      <template #body="slotProps">
        <Checkbox
          id="egress"
          inputId="egress"
          v-model="slotProps.data.egress"
          :binary="true"
          :disabled="!slotProps.data.editable"
        />
      </template>
    </Column>
    <Column field="created_at" header="Created">
      <template #body="slotProps">{{
        slotProps.data.created_at.fromNow()
      }}</template>
    </Column>
    <template #expansion="slotProps">
      <PVButton
        v-if="!true && !slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-primary"
        icon="pi pi-pencil"
        label="Edit"
        @click="startEdit(slotProps.data)"
      ></PVButton>
      <PVButton
        v-if="!true && slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-success"
        icon="pi pi-check"
        label="Save"
        @click="stopEdit(slotProps.data)"
      ></PVButton>
      <PVButton
        v-if="!true && slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-primary"
        icon="pi pi-ban"
        label="Cancel"
        style="margin-left: 0.5em"
        @click="cancelEdit(slotProps.data)"
      ></PVButton>
      <PVButton
        class="p-button-raised p-button-rounded p-button-danger"
        icon="pi pi-trash"
        label="Delete"
        style="margin-left: 0.5em"
        v-if="!slotProps.data.editable"
        @click="deletePeer(slotProps.data)"
      ></PVButton>
    </template>
  </DataTable>
</template>

<script>
import Button from 'primevue/button';
import Checkbox from 'primevue/checkbox';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import moment from 'moment';

import API from '@/services/API';
import { getWebsocketURI } from '@/services/util';

export default {
  name: 'RepeaterTable',
  props: {
    admin: Boolean,
  },
  components: {
    PVButton: Button,
    Checkbox,
    DataTable,
    Column,
  },
  data: function() {
    return {
      peers: [],
      expandedRows: [],
      socket: null,
      editablePeers: 0,
      refresh: null,
      totalRecords: 0,
      first: 0,
      loading: false,
    };
  },
  mounted() {
    this.fetchData();
    if (!this.$props.admin) {
      this.socket = new WebSocket(getWebsocketURI() + '/peers');
      this.mapSocketEvents();
    }
  },
  unmounted() {
    clearInterval(this.refresh);
    if (this.socket) {
      this.socket.close();
    }
  },
  methods: {
    onPage(event) {
      this.loading = true;
      this.first = event.page * event.rows;
      this.fetchData(event.page + 1, event.rows);
    },
    fetchData(page = 1, limit = 10) {
      if (!this.editablePeers > 0) {
        if (this.$props.admin) {
          API.get(`/peers?limit=${limit}&page=${page}`)
            .then((res) => {
              this.peers = this.cleanData(res.data.peers);
              this.totalRecords = res.data.total;
              this.loading = false;
            })
            .catch((err) => {
              console.error(err);
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
            });
        }
      }
    },
    cleanData(data) {
      const copyData = JSON.parse(JSON.stringify(data));
      for (let i = 0; i < copyData.length; i++) {
        copyData[i].created_at = moment(copyData[i].created_at);
        copyData[i].last_ping_time = moment(copyData[i].last_ping_time);
        copyData[i].editable = false;
      }
      return copyData;
    },
    startEdit(peer) {
      peer.editable = true;
      this.editablePeers++;
    },
    cancelEdit(peer) {
      peer.editable = false;
      this.editablePeers--;
      if (this.editablePeers == 0) {
        this.fetchData();
      }
    },
    stopEdit(_peer) {},
    deletePeer(peer) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: 'Are you sure you want to delete this peer?',
        header: 'Delete Peer',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.delete('/peers/' + peer.id)
            .then((_res) => {
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
      this.socket.addEventListener('open', (_event) => {
        console.log('Connected to peers websocket');
        this.socket.send('PING');
      });

      this.socket.addEventListener('error', (event) => {
        console.error('Error from peers websocket', event);
        this.socket.close();
        this.socket = new WebSocket(getWebsocketURI() + '/peers');
        this.mapSocketEvents();
      });

      this.socket.addEventListener('message', (event) => {
        if (event.data == 'PONG') {
          setTimeout(() => {
            this.socket.send('PING');
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
