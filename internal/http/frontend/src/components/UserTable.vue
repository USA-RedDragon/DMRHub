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
    :value="users"
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
    <Column :expander="true" v-if="!this.$props.approval" />
    <Column field="id" header="DMR ID"></Column>
    <Column field="callsign" header="Callsign"></Column>
    <Column field="username" header="Username"></Column>
    <Column
      field="approved"
      :header="this.$props.approval ? 'Approve?' : 'Approved'"
    >
      <template #body="slotProps" v-if="this.$props.approval">
        <PVButton
          label="Approve"
          class="p-button-raised p-button-rounded"
          @click="handleApprovePage(slotProps.data)"
        />
      </template>
      <template #body="slotProps" v-else>
        <span v-if="slotProps.data.approved">Yes</span>
        <span v-else>No</span>
      </template>
    </Column>
    <Column field="suspended" header="Suspend?" v-if="!this.$props.approval">
      <template #body="slotProps">
        <PVCheckbox
          v-model="slotProps.data.suspended"
          :binary="true"
          @change="handleSuspend($event, slotProps.data)"
        />
      </template>
    </Column>
    <Column field="admin" header="Admin?" v-if="!this.$props.approval">
      <template #body="slotProps">
        <PVCheckbox
          v-model="slotProps.data.admin"
          :binary="true"
          @change="handleAdmin($event, slotProps.data)"
        />
      </template>
    </Column>
    <Column
      field="repeaters"
      header="Repeater Count"
      v-if="!this.$props.approval"
    ></Column>
    <Column field="created_at" header="Created">
      <template #body="slotProps">{{
        slotProps.data.created_at.fromNow()
      }}</template>
    </Column>
    <template #expansion="slotProps">
      <PVButton
        class="p-button-raised p-button-rounded p-button-primary"
        icon="pi pi-pencil"
        label="Edit"
        @click="editUser(slotProps.data)"
      ></PVButton>
      <PVButton
        class="p-button-raised p-button-rounded p-button-danger"
        icon="pi pi-trash"
        label="Delete"
        style="margin-left: 0.5em"
        @click="deleteUser(slotProps.data)"
      ></PVButton>
    </template>
  </DataTable>
</template>

<script>
import Button from 'primevue/button/sfc';
import Checkbox from 'primevue/checkbox/sfc';
import DataTable from 'primevue/datatable/sfc';
import Column from 'primevue/column/sfc';

import moment from 'moment';

import { mapStores } from 'pinia';
import { useUserStore, useSettingsStore } from '@/store';

import API from '@/services/API';

export default {
  name: 'UserTable',
  props: {
    approval: Boolean,
  },
  components: {
    PVButton: Button,
    PVCheckbox: Checkbox,
    DataTable,
    Column,
  },
  data: function() {
    return {
      users: [],
      expandedRows: [],
      refresh: null,
      loading: false,
      totalRecords: 0,
    };
  },
  mounted() {
    this.fetchData();
    this.refresh = setInterval(
      this.fetchData,
      this.settingsStore.refreshInterval,
    );
  },
  unmounted() {
    clearInterval(this.refresh);
  },
  methods: {
    onPage(event) {
      this.loading = true;
      this.fetchData(event.page + 1, event.rows);
    },
    fetchData(page = 1, limit = 10) {
      if (this.$props.approval) {
        API.get(`/users/unapproved?page=${page}&limit=${limit}`)
          .then((res) => {
            for (let i = 0; i < res.data.users.length; i++) {
              res.data.users[i].repeaters = res.data.users[i].repeaters.length;

              res.data.users[i].created_at = moment(
                res.data.users[i].created_at,
              );
            }
            this.users = res.data.users;
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
          });
      } else {
        API.get(`/users?page=${page}&limit=${limit}`)
          .then((res) => {
            for (let i = 0; i < res.data.users.length; i++) {
              res.data.users[i].repeaters = res.data.users[i].repeaters.length;

              res.data.users[i].created_at = moment(
                res.data.users[i].created_at,
              );
            }
            this.users = res.data.users;
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
          });
      }
    },
    handleApprovePage(user) {
      this.$confirm.require({
        message: 'Are you sure you want to approve this user?',
        header: 'Approve User',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.post('/users/approve/' + user.id, {})
            .then((_res) => {
              // Refresh user data
              this.fetchData();
              this.$toast.add({
                summary: 'Confirmed',
                severity: 'success',
                detail: `User ${user.id} approved`,
                life: 3000,
              });
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                summary: 'Error',
                severity: 'error',
                detail: `Error approving user ${user.id}`,
                life: 3000,
              });
            });
        },
        reject: () => {},
      });
    },
    handleSuspend(event, user) {
      const action = user.suspended ? 'suspend' : 'unsuspend';
      const actionVerb = user.suspended ? 'suspended' : 'unsuspended';
      // Don't allow the user to uncheck the admin box
      API.post(`/users/${action}/${user.id}`, {})
        .then(() => {
          this.$toast.add({
            summary: 'Confirmed',
            severity: 'success',
            detail: `User ${user.id} ${actionVerb}`,
            life: 3000,
          });
          this.fetchData();
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: 'An unknown error occurred',
              life: 3000,
            });
          }
          this.fetchData();
        });
    },
    handleAdmin(event, user) {
      const action = user.admin ? 'promote' : 'demote';
      const actionVerb = user.admin ? 'promoted' : 'demoted';
      // Don't allow the user to uncheck the admin box
      if (this.userStore.id == 999999) {
        API.post(`/users/${action}/${user.id}`, {})
          .then(() => {
            this.$toast.add({
              summary: 'Confirmed',
              severity: 'success',
              detail: `User ${user.id} ${actionVerb}`,
              life: 3000,
            });
            this.fetchData();
          })
          .catch((err) => {
            console.error(err);
            if (err.response && err.response.data && err.response.data.error) {
              this.$toast.add({
                severity: 'error',
                summary: 'Error',
                detail: err.response.data.error,
                life: 3000,
              });
            } else {
              this.$toast.add({
                severity: 'error',
                summary: 'Error',
                detail: 'An unknown error occurred',
                life: 3000,
              });
            }
            this.fetchData();
          });
      } else {
        this.$toast.add({
          summary: 'Only the System Admin can do this.',
          severity: 'error',
          detail: `Standard Admins cannot promote other users.`,
          life: 3000,
        });
        this.fetchData();
      }
    },
    editUser(_user) {
      this.$toast.add({
        summary: 'Not Implemented',
        severity: 'error',
        detail: `Users cannot be edited yet.`,
        life: 3000,
      });
    },
    deleteUser(user) {
      if (this.userStore.id == 999999) {
        this.$confirm.require({
          message: 'Are you sure you want to delete this user?',
          header: 'Delete User',
          icon: 'pi pi-exclamation-triangle',
          acceptClass: 'p-button-danger',
          accept: () => {
            API.delete('/users/' + user.id)
              .then((_res) => {
                this.$toast.add({
                  summary: 'Confirmed',
                  severity: 'success',
                  detail: `User ${user.id} deleted`,
                  life: 3000,
                });
                this.fetchData();
              })
              .catch((err) => {
                console.error(err);
                this.$toast.add({
                  severity: 'error',
                  summary: 'Error',
                  detail: `Error deleting user ${user.id}`,
                  life: 3000,
                });
              });
          },
          reject: () => {},
        });
      } else {
        this.$toast.add({
          summary: 'Only the System Admin can do this.',
          severity: 'error',
          detail: `Standard admins cannot delete other users.`,
          life: 3000,
        });
      }
    },
  },
  computed: {
    ...mapStores(useUserStore, useSettingsStore),
  },
};
</script>

<style scoped></style>
