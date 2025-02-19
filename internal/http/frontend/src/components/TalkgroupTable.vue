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
  <DataTable
    v-model:expandedRows="expandedRows"
    :value="talkgroups"
    :lazy="true"
    :first="first"
    :paginator="true"
    :rows="10"
    :totalRecords="totalRecords"
    :loading="loading"
    :scrollable="true"
    @page="onPage($event)"
  >
    <template v-if="this.$props.admin" #header>
      <div class="table-header-container">
        <RouterLink to="/admin/talkgroups/new">
          <PVButton
            class="p-button-raised p-button-rounded p-button-success"
            icon="pi pi-plus"
            label="Add New Talkgroup"
          />
        </RouterLink>
      </div>
    </template>
    <Column :expander="true" v-if="this.$props.admin || this.$props.owner" />
    <Column field="id" header="Channel"></Column>
    <Column field="name" header="Name">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">{{ slotProps.data.name }}</span>
        <InputText
          v-if="slotProps.data.editable"
          v-model="slotProps.data.name"
        />
      </template>
    </Column>
    <Column field="description" header="Description">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">{{
          slotProps.data.description
        }}</span>
        <InputText
          v-if="slotProps.data.editable"
          v-model="slotProps.data.description"
        />
      </template>
    </Column>
    <Column v-if="!this.$props.owner" field="admins" header="Admins">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span v-if="slotProps.data.admins.length == 0">None</span>
          <span
            v-else
            v-bind:key="admin.id"
            v-for="admin in slotProps.data.admins"
          >
            {{ admin.display }}&nbsp;
          </span>
        </span>
        <span class="p-float-label" v-else>
          <MultiSelect
            id="admins"
            v-model="slotProps.data.admins"
            :options="allUsers"
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
          <label for="admins">Admins</label>
        </span>
      </template>
    </Column>
    <Column field="ncos" header="Net Control Operators">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span v-if="!slotProps.data.ncos || slotProps.data.ncos.length == 0"
            >None</span
          >
          <span v-else v-bind:key="nco.id" v-for="nco in slotProps.data.ncos">
            {{ nco.display }}&nbsp;
          </span>
        </span>
        <span class="p-float-label" v-else>
          <MultiSelect
            id="ncos"
            v-model="slotProps.data.ncos"
            :options="allUsers"
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
          <label for="ncos">Net Control Operators</label>
        </span>
      </template>
    </Column>
    <Column field="created_at" header="Created">
      <template #body="slotProps"><span v-tooltip="slotProps.data.created_at.toString()">{{
        slotProps.data.created_at.fromNow()
      }}</span></template>
    </Column>
    <template
      v-if="this.$props.admin || this.$props.owner"
      #expansion="slotProps"
    >
      <PVButton
        v-if="!slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-primary"
        icon="pi pi-pencil"
        label="Edit"
        @click="startEdit(slotProps.data)"
      ></PVButton>
      <PVButton
        v-if="slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-success"
        icon="pi pi-check"
        label="Save"
        @click="saveTalkgroup(slotProps.data)"
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
        v-if="this.$props.admin && !slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-danger"
        icon="pi pi-trash"
        label="Delete"
        style="margin-left: 0.5em"
        @click="deleteTalkgroup(slotProps.data)"
      ></PVButton>
    </template>
  </DataTable>
</template>

<script>
import Button from 'primevue/button';
import DataTable from 'primevue/datatable';
import Column from 'primevue/column';
import InputText from 'primevue/inputtext';
import MultiSelect from 'primevue/multiselect';

import moment from 'moment';

import { mapStores } from 'pinia';
import { useSettingsStore } from '@/store';

import API from '@/services/API';

export default {
  name: 'TalkgroupTable',
  props: {
    admin: Boolean,
    owner: Boolean,
  },
  components: {
    PVButton: Button,
    DataTable,
    Column,
    InputText,
    MultiSelect,
  },
  data: function() {
    return {
      talkgroups: [],
      expandedRows: [],
      editableTalkgroups: 0,
      totalRecords: 0,
      first: 0,
      loading: false,
      allUsers: [],
    };
  },
  mounted() {
    this.fetchData();
  },
  unmounted() {
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
      if (this.editableTalkgroups > 0) {
        return;
      }
      if (this.$props.owner || this.$props.admin) {
        API.get('/users?limit=none')
          .then((res) => {
            let parrotIndex = -1;
            for (let i = 0; i < res.data.users.length; i++) {
              res.data.users[
                i
              ].display = `${res.data.users[i].id} - ${res.data.users[i].callsign}`;
              // Remove user with id 9990 (parrot)
              if (res.data.users[i].id === 9990) {
                parrotIndex = i;
              }
            }
            if (parrotIndex !== -1) {
              res.data.users.splice(parrotIndex, 1);
            }
            this.allUsers = res.data.users;
          })
          .catch((err) => {
            console.error(err);
          });
      }
      if (this.$props.owner) {
        API.get(`/talkgroups/my?limit=${limit}&page=${page}`)
          .then((res) => {
            this.talkgroups = this.cleanData(res.data.talkgroups);
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
          });
      } else {
        API.get(`/talkgroups?limit=${limit}&page=${page}`)
          .then((res) => {
            this.talkgroups = this.cleanData(res.data.talkgroups);
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
          });
      }
    },
    cleanData(data) {
      const copyData = JSON.parse(JSON.stringify(data));

      for (let i = 0; i < copyData.length; i++) {
        copyData[i].created_at = moment(copyData[i].created_at);
        copyData[i].editable = false;

        if (copyData[i].admins) {
          for (let j = 0; j < copyData[i].admins.length; j++) {
            copyData[i].admins[
              j
            ].display = `${copyData[i].admins[j].id} - ${copyData[i].admins[j].callsign}`;
          }
        }

        if (copyData[i].ncos) {
          for (let j = 0; j < copyData[i].ncos.length; j++) {
            copyData[i].ncos[
              j
            ].display = `${copyData[i].ncos[j].id} - ${copyData[i].ncos[j].callsign}`;
          }
        }
      }
      return copyData;
    },
    startEdit(talkgroup) {
      this.editableTalkgroups++;
      talkgroup.editable = true;
    },
    cancelEdit(talkgroup) {
      talkgroup.editable = false;
      this.editableTalkgroups--;
      if (this.editableTalkgroups == 0) {
        this.fetchData();
      }
    },
    deleteTalkgroup(talkgroup) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: 'Are you sure you want to delete this talkgroup?',
        header: 'Delete Talkgroup',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.delete('/talkgroups/' + talkgroup.id)
            .then((_res) => {
              this.$toast.add({
                summary: 'Confirmed',
                severity: 'success',
                detail: `Talkgroup ${talkgroup.id} deleted`,
                life: 3000,
              });
              this.fetchData();
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                severity: 'error',
                summary: 'Error',
                detail: `Error deleting talkgroup ${talkgroup.id}`,
                life: 3000,
              });
            });
        },
        reject: () => {},
      });
    },
    saveTalkgroup(talkgroup) {
      API.patch('/talkgroups/' + talkgroup.id, {
        name: talkgroup.name,
        description: talkgroup.description,
      })
        .then((_res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: 'Talkgroup updated',
            life: 3000,
          });
          API.post(`/talkgroups/${talkgroup.id}/admins`, {
            user_ids: talkgroup.admins.map((admin) => admin.id),
          })
            .then((_res) => {
              API.post(`/talkgroups/${talkgroup.id}/ncos`, {
                user_ids: talkgroup.ncos.map((nco) => nco.id),
              })
                .then((_res) => {
                  talkgroup.editable = false;
                  this.editableTalkgroups--;
                  if (this.editableTalkgroups == 0) {
                    this.fetchData();
                  }
                })
                .catch((err) => {
                  console.error(err);
                  if (
                    err.response &&
                    err.response.data &&
                    err.response.data.error
                  ) {
                    this.$toast.add({
                      severity: 'error',
                      summary: 'Error',
                      detail:
                        'Failed to update talkgroup admins: ' +
                        err.response.data.error,
                      life: 3000,
                    });
                    return;
                  } else {
                    this.$toast.add({
                      severity: 'error',
                      summary: 'Error',
                      detail: 'Failed to update talkgroup admins',
                      life: 3000,
                    });
                  }
                });
            })
            .catch((err) => {
              console.error(err);
              if (
                err.response &&
                err.response.data &&
                err.response.data.error
              ) {
                this.$toast.add({
                  severity: 'error',
                  summary: 'Error',
                  detail:
                    'Failed to update talkgroup admins: ' +
                    err.response.data.error,
                  life: 3000,
                });
                return;
              } else {
                this.$toast.add({
                  severity: 'error',
                  summary: 'Error',
                  detail: 'Failed to update talkgroup admins',
                  life: 3000,
                });
              }
            });
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: 'Failed to update talkgroup: ' + err.response.data.error,
              life: 3000,
            });
            return;
          } else {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: 'Failed to update talkgroup',
              life: 3000,
            });
          }
        });
    },
  },
  computed: {
    ...mapStores(useSettingsStore),
  },
};
</script>

<style scoped>
.table-header-container {
  display: flex;
  justify-content: flex-end;
}
</style>
