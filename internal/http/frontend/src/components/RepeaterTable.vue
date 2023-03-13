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
    :value="repeaters"
    v-model:expandedRows="expandedRows"
    dataKey="id"
    :lazy="true"
    :paginator="true"
    :rows="10"
    :totalRecords="totalRecords"
    :loading="loading"
    :scrollable="true"
    @page="onPage($event)"
  >
    <template #header v-if="!this.$props.admin">
      <div class="table-header-container">
        <RouterLink to="/repeaters/new">
          <PVButton
            class="p-button-raised p-button-rounded p-button-success"
            icon="pi pi-plus"
            label="Enroll New Repeater"
          />
        </RouterLink>
      </div>
    </template>
    <Column :expander="true" />
    <Column field="id" header="DMR Radio ID"></Column>
    <Column field="connected_time" header="Last Connected">
      <template #body="slotProps">
        <span v-if="slotProps.data.connected_time.year() != 0">
          {{ slotProps.data.connected_time.fromNow() }}
        </span>
        <span v-else>Never</span>
      </template>
    </Column>
    <Column field="last_ping_time" header="Last Ping">
      <template #body="slotProps">
        <span v-if="slotProps.data.last_ping_time.year() != 0">
          {{ slotProps.data.last_ping_time.fromNow() }}
        </span>
        <span v-else>Never</span>
      </template>
    </Column>
    <Column field="ts1_static_talkgroups" header="TS1 Static TGs">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span
            v-if="
              slotProps.data.ts1_static_talkgroups.length == 0 ||
              slotProps.data.slots == 1
            "
            >None</span
          >
          <span
            v-else
            v-bind:key="tg.id"
            class="chips"
            v-for="tg in slotProps.data.ts1_static_talkgroups"
          >
            <PVChip :label="tg.id + ' - ' + tg.name"></PVChip>
          </span>
        </span>
        <span v-else>
          <span v-if="slotProps.data.slots != 1" class="p-float-label">
            <MultiSelect
              id="ts1_static_talkgroups"
              v-model="slotProps.data.ts1_static_talkgroups"
              :options="this.talkgroups"
              :filter="true"
              optionLabel="display"
              display="chip"
            >
              <template #chip="slotProps">
                {{ slotProps.value.display }}
              </template>
              <template #option="slotProps">
                {{ slotProps.option.display }}
              </template>
            </MultiSelect>
            <label for="ts1_static_talkgroups">TGs</label>
          </span>
        </span>
      </template>
    </Column>
    <Column field="ts2_static_talkgroups" header="TS2 Static TGs">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span v-if="slotProps.data.ts2_static_talkgroups.length == 0"
            >None</span
          >
          <span
            v-else
            v-bind:key="tg.id"
            class="chips"
            v-for="tg in slotProps.data.ts2_static_talkgroups"
          >
            <PVChip :label="tg.id + ' - ' + tg.name"></PVChip>
          </span>
        </span>
        <span class="p-float-label" v-else>
          <MultiSelect
            id="ts2_static_talkgroups"
            v-model="slotProps.data.ts2_static_talkgroups"
            :options="this.talkgroups"
            :filter="true"
            optionLabel="display"
            display="chip"
          >
            <template #chip="slotProps">
              {{ slotProps.value.display }}
            </template>
            <template #option="slotProps">
              {{ slotProps.option.display }}
            </template>
          </MultiSelect>
          <label for="ts2_static_talkgroups">TGs</label>
        </span>
      </template>
    </Column>
    <Column field="ts1_dynamic_talkgroup" header="TS1 Dynamic TG">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span
            v-if="
              slotProps.data.ts1_dynamic_talkgroup.id == 0 ||
              slotProps.data.slots == 1
            "
            >None</span
          >
          <PVChip
            v-else
            :label="
              slotProps.data.ts1_dynamic_talkgroup.id +
              ' - ' +
              slotProps.data.ts1_dynamic_talkgroup.name
            "
          ></PVChip>
        </span>
        <span v-else>
          <span v-if="slotProps.data.slots != 1" class="p-float-label">
            <Dropdown
              id="ts1_dynamic_talkgroup"
              v-model="slotProps.data.ts1_dynamic_talkgroup"
              :options="this.talkgroups"
              :filter="true"
              optionLabel="display"
              display="chip"
            >
              <template #value="slotProps">
                <PVChip
                  :label="slotProps.value.display"
                  v-if="slotProps.value.id != 0"
                ></PVChip>
              </template>
              <template #option="slotProps">
                {{ slotProps.option.display }}
              </template>
            </Dropdown>
            <label for="ts1_dynamic_talkgroup">TG</label>
          </span>
        </span>
      </template>
    </Column>
    <Column field="ts2_dynamic_talkgroup" header="TS2 Dynamic TG">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span v-if="slotProps.data.ts2_dynamic_talkgroup.id == 0">
            None
          </span>
          <PVChip
            v-else
            :label="
              slotProps.data.ts2_dynamic_talkgroup.id +
              ' - ' +
              slotProps.data.ts2_dynamic_talkgroup.name
            "
          ></PVChip>
        </span>
        <span class="p-float-label" v-else>
          <Dropdown
            id="ts2_dynamic_talkgroup"
            v-model="slotProps.data.ts2_dynamic_talkgroup"
            :options="this.talkgroups"
            :filter="true"
            optionLabel="display"
            display="chip"
          >
            <template #value="slotProps">
              <PVChip
                :label="slotProps.value.display"
                v-if="slotProps.value.id != 0"
              ></PVChip>
            </template>
            <template #option="slotProps">
              {{ slotProps.option.display }}
            </template>
          </Dropdown>
          <label for="ts2_dynamic_talkgroup">TG</label>
        </span>
      </template>
    </Column>
    <Column field="hotspot" header="Hotspot"></Column>
    <Column field="created_at" header="Created">
      <template #body="slotProps">{{
        slotProps.data.created_at.fromNow()
      }}</template>
    </Column>
    <template #expansion="slotProps">
      <PVButton
        v-if="!slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-primary"
        icon="pi pi-pencil"
        label="Edit Talkgroups"
        @click="startEdit(slotProps.data)"
      ></PVButton>
      <PVButton
        v-if="slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-success"
        icon="pi pi-check"
        label="Save Talkgroups"
        @click="saveTalkgroups(slotProps.data)"
      ></PVButton>
      <PVButton
        class="p-button-raised p-button-rounded p-button-secondary"
        icon="pi pi-link"
        label="Unlink Dynamic TS1"
        v-if="slotProps.data.slots != 1"
        style="margin-left: 0.5em"
        @click="unlink(1, slotProps.data)"
      ></PVButton>
      <PVButton
        class="p-button-raised p-button-rounded p-button-secondary"
        icon="pi pi-link"
        label="Unlink Dynamic TS2"
        style="margin-left: 0.5em"
        @click="unlink(2, slotProps.data)"
      ></PVButton>
      <PVButton
        v-if="slotProps.data.editable"
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
        @click="deleteRepeater(slotProps.data)"
      ></PVButton>
    </template>
  </DataTable>
</template>

<script>
import Button from 'primevue/button/sfc';
import DataTable from 'primevue/datatable/sfc';
import Column from 'primevue/column/sfc';
import MultiSelect from 'primevue/multiselect/sfc';
import Dropdown from 'primevue/dropdown/sfc';
import Chip from 'primevue/chip/sfc';
import moment from 'moment';

import API from '@/services/API';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

export default {
  name: 'RepeaterTable',
  props: {
    admin: Boolean,
  },
  components: {
    PVButton: Button,
    DataTable,
    Column,
    MultiSelect,
    Dropdown,
    PVChip: Chip,
  },
  data: function() {
    return {
      talkgroups: [],
      repeaters: [],
      expandedRows: [],
      socket: null,
      editableRepeaters: 0,
      refresh: null,
      totalRecords: 0,
      loading: false,
    };
  },
  mounted() {
    this.fetchData();
    if (!this.$props.admin) {
      this.socket = ws.connect(getWebsocketURI() + '/repeaters', this.onWebsocketMessage);
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
      this.fetchData(event.page + 1, event.rows);
    },
    fetchData(page = 1, limit = 10) {
      API.get('/talkgroups?limit=none')
        .then((res) => {
          this.talkgroups = res.data.talkgroups;
          let parrotIndex = -1;
          for (let i = 0; i < this.talkgroups.length; i++) {
            this.talkgroups[i].display =
              this.talkgroups[i].id + ' - ' + this.talkgroups[i].name;

            if (this.talkgroups[i].id == 9990) {
              parrotIndex = i;
            }
          }
          // Remove i from the array
          if (parrotIndex > -1) {
            this.talkgroups.splice(parrotIndex, 1);
          }
        })
        .catch((err) => {
          console.log(err);
        });

      if (!this.editableRepeaters > 0) {
        if (this.$props.admin) {
          API.get(`/repeaters?limit=${limit}&page=${page}`)
            .then((res) => {
              this.repeaters = this.cleanData(res.data.repeaters);
              this.totalRecords = res.data.total;
              this.loading = false;
            })
            .catch((err) => {
              console.error(err);
            });
        } else {
          API.get(`/repeaters/my?limit=${limit}&page=${page}`)
            .then((res) => {
              this.repeaters = this.cleanData(res.data.repeaters);
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
        copyData[i].connected_time = moment(copyData[i].connected_time);

        copyData[i].created_at = moment(copyData[i].created_at);

        copyData[i].last_ping_time = moment(copyData[i].last_ping_time);
        copyData[i].editable = false;

        for (let j = 0; j < copyData[i].ts1_static_talkgroups.length; j++) {
          copyData[i].ts1_static_talkgroups[j].display =
            copyData[i].ts1_static_talkgroups[j].id +
            ' - ' +
            copyData[i].ts1_static_talkgroups[j].name;
        }

        for (let j = 0; j < copyData[i].ts2_static_talkgroups.length; j++) {
          copyData[i].ts2_static_talkgroups[j].display =
            copyData[i].ts2_static_talkgroups[j].id +
            ' - ' +
            copyData[i].ts2_static_talkgroups[j].name;
        }

        copyData[i].ts1_dynamic_talkgroup.display =
          copyData[i].ts1_dynamic_talkgroup.id +
          ' - ' +
          copyData[i].ts1_dynamic_talkgroup.name;

        copyData[i].ts2_dynamic_talkgroup.display =
          copyData[i].ts2_dynamic_talkgroup.id +
          ' - ' +
          copyData[i].ts2_dynamic_talkgroup.name;
      }
      return copyData;
    },
    startEdit(repeater) {
      repeater.editable = true;
      this.editableRepeaters++;
    },
    cancelEdit(repeater) {
      repeater.editable = false;
      this.editableRepeaters--;
      if (this.editableRepeaters == 0) {
        this.fetchData();
      }
    },
    saveTalkgroups(repeater) {
      API.post(`/repeaters/${repeater.id}/talkgroups`, {
        ts1_dynamic_talkgroup: repeater.ts1_dynamic_talkgroup,
        ts2_dynamic_talkgroup: repeater.ts2_dynamic_talkgroup,
        ts1_static_talkgroups: repeater.ts1_static_talkgroups,
        ts2_static_talkgroups: repeater.ts2_static_talkgroups,
      })
        .then((_res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: `Talkgroups updated for repeater ${repeater.id}`,
            life: 3000,
          });
          repeater.editable = false;
          this.editableRepeaters--;
          if (this.editableRepeaters == 0) {
            this.fetchData();
          }
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: 'Failed to update repeater: ' + err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: `Error updating talkgroups for repeater ${repeater.id}`,
              life: 3000,
            });
          }
        });
    },
    unlink(ts, repeater) {
      // API call: POST /repeaters/:id/unlink/dynamic/:ts/:tg
      let tg = 0;
      if (ts == 1) {
        tg = repeater.ts1_dynamic_talkgroup.id;
      } else if (ts == 2) {
        tg = repeater.ts2_dynamic_talkgroup.id;
      }
      API.post(`/repeaters/${repeater.id}/unlink/dynamic/${ts}/${tg}`, {})
        .then((_res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: `Talkgroup ${tg} unlinked on TS${ts} for repeater ${repeater.id}`,
            life: 3000,
          });
          this.fetchData();
        })
        .catch((err) => {
          console.error(err);
          this.$toast.add({
            severity: 'error',
            summary: 'Error',
            detail: `Error unlinking talkgroup for repeater ${repeater.id}`,
            life: 3000,
          });
        });
    },
    deleteRepeater(repeater) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: 'Are you sure you want to delete this repeater?',
        header: 'Delete Repeater',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.delete('/repeaters/' + repeater.id)
            .then((_res) => {
              this.$toast.add({
                summary: 'Confirmed',
                severity: 'success',
                detail: `Repeater ${repeater.id} deleted`,
                life: 3000,
              });
              this.fetchData();
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                severity: 'error',
                summary: 'Error',
                detail: `Error deleting repeater ${repeater.id}`,
                life: 3000,
              });
            });
        },
        reject: () => {},
      });
    },
    onWebsocketMessage(_event) {
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
