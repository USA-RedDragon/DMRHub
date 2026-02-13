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
  <DataTable
    :columns="columns"
    :data="users"
    :loading="loading"
    :loading-text="'Loading...'"
    :empty-text="'No users found'"
    :manual-pagination="true"
    :page-index="page - 1"
    :page-size="rows"
    :page-count="totalPages"
    @update:page-index="handlePageIndexUpdate"
    @update:page-size="handlePageSizeUpdate"
  />
</template>

<script lang="ts">
import type { ColumnDef } from '@tanstack/vue-table';
import { h, type RendererElement } from 'vue';
import { DataTable } from '@/components/ui/data-table';
import { Input } from '@/components/ui/input';

import { format, formatDistanceToNowStrict } from 'date-fns';

import { mapStores } from 'pinia';
import { useUserStore, useSettingsStore } from '@/store';

import API from '@/services/API';
import { buttonVariants } from '@/components/ui/button';

type UserRow = {
  id: number;
  callsign: string;
  username: string;
  approved: boolean;
  suspended: boolean;
  admin: boolean;
  repeaters: number;
  created_at: string | Date;
  editing: boolean;
};

export default {
  name: 'UserTable',
  props: {
    approval: Boolean,
  },
  components: {
    DataTable,
  },
  data: function() {
    return {
      users: [] as UserRow[],
      loading: false,
      totalRecords: 0,
      page: 1,
      rows: 30,
    };
  },
  mounted() {
    this.fetchData();
  },
  unmounted() {
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
      if (this.$props.approval) {
        API.get(`/users/unapproved?page=${page}&limit=${limit}`)
          .then((res) => {
            for (let i = 0; i < res.data.users.length; i++) {
              res.data.users[i].repeaters = res.data.users[i].repeaters.length;

              res.data.users[i].created_at = new Date(res.data.users[i].created_at);
            }
            this.users = res.data.users;
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
            this.loading = false;
          });
      } else {
        API.get(`/users?page=${page}&limit=${limit}`)
          .then((res) => {
            for (let i = 0; i < res.data.users.length; i++) {
              res.data.users[i].repeaters = res.data.users[i].repeaters.length;

              res.data.users[i].editing = false;

              res.data.users[i].created_at = new Date(res.data.users[i].created_at);
            }
            this.users = res.data.users;
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
            this.loading = false;
          });
      }
    },
    handleApprovePage(user: UserRow) {
      this.$confirm.require({
        message: 'Are you sure you want to approve this user?',
        header: 'Approve User',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.post('/users/approve/' + user.id, {})
            .then(() => {
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
    relativeTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return formatDistanceToNowStrict(date, { addSuffix: true });
    },
    absoluteTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return format(date, 'yyyy-MM-dd HH:mm:ss');
    },
    handleSuspend(_event: Event, user: UserRow) {
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
    handleAdmin(_event: Event, user: UserRow) {
      const action = user.admin ? 'promote' : 'demote';
      const actionVerb = user.admin ? 'promoted' : 'demoted';
      // Don't allow the user to uncheck the admin box
      if (this.userStore.superAdmin) {
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
    editUser(userID: number) {
      for (let i = 0; i < this.users.length; i++) {
        const u = this.users[i];
        if (u && u.id === userID) {
          u.editing = true;
          return;
        }
      }
    },
    finishEditingUser(user: UserRow) {
      // Send PATCH
      API.patch(`/users/${user.id}`, {
        callsign: user.callsign,
        username: user.username,
      })
        .then(() => {
          for (let i = 0; i < this.users.length; i++) {
            const u = this.users[i];
            if (u && u.id === user.id) {
              u.editing = false;
              break;
            }
          }
          this.$toast.add({
            summary: 'Confirmed',
            severity: 'success',
            detail: `User ${user.id} updated`,
            life: 3000,
          });
          this.fetchData();
        })
        .catch((err) => {
          console.error(err);
          this.$toast.add({
            severity: 'error',
            summary: 'Error',
            detail: `Error updating user ${user.id}`,
            life: 3000,
          });
        });
    },
    cancelEditingUser(user: UserRow) {
      user.editing = false;
      this.fetchData(this.page, this.rows);
    },
    deleteUser(user: UserRow) {
      if (this.userStore.superAdmin) {
        this.$confirm.require({
          message: 'Are you sure you want to delete this user?',
          header: 'Delete User',
          icon: 'pi pi-exclamation-triangle',
          acceptClass: 'p-button-danger',
          accept: () => {
            API.delete('/users/' + user.id)
              .then(() => {
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
    columns() {
      const columns: ColumnDef<UserRow, RendererElement>[] = []
      if (!this.approval) {
        columns.push({
          accessorKey: 'actions',
          header: '',
          cell: ({ row }: { row: { original: UserRow } }) => {
            const user = row.original;
            const actionButton = (label: string, onClick: () => void) => {
              return h('button', {
                class: buttonVariants({ variant: 'outline', size: 'sm' }),
                onClick,
              }, label);
            };
            return h('div', { class: 'flex gap-2' }, [
              !user.editing
                ? actionButton('Edit', () => this.editUser(user.id))
                : actionButton('Save Changes', () => this.finishEditingUser(user)),
              user.editing
                ? actionButton('Cancel', () => this.cancelEditingUser(user))
                : actionButton('Delete', () => this.deleteUser(user)),
            ]);
          },
        });
      }

      columns.push({
          accessorKey: 'id',
          header: 'DMR ID',
          cell: ({ row }: { row: { original: UserRow } }) => `${row.original.id}`,
        },
        {
          accessorKey: 'callsign',
          header: 'Callsign',
          cell: ({ row }: { row: { original: UserRow } }) => {
            const user = row.original;
            if (!user.editing) {
              return user.callsign;
            }
            return h(Input, {
              type: 'text',
              modelValue: user.callsign,
              'onUpdate:modelValue': (value: string | number) => {
                user.callsign = String(value);
              },
            });
          },
        },
        {
          accessorKey: 'username',
          header: 'Username',
          cell: ({ row }: { row: { original: UserRow } }) => {
            const user = row.original;
            if (!user.editing) {
              return user.username;
            }
            return h(Input, {
              type: 'text',
              modelValue: user.username,
              'onUpdate:modelValue': (value: string | number) => {
                user.username = String(value);
              },
            });
          },
        },
        {
          accessorKey: 'approved',
          header: this.approval ? 'Approve?' : 'Approved',
          cell: ({ row }: { row: { original: UserRow } }) => {
            const user = row.original;
            if (this.approval) {
              return h('button', {
                class: buttonVariants({ variant: 'outline', size: 'sm' }),
                onClick: () => this.handleApprovePage(user),
              }, 'Approve');
            }
            return user.approved ? 'Yes' : 'No';
          },
        },
      );

      if (!this.approval) {
        columns.push(
          {
            accessorKey: 'suspended',
            header: 'Suspend?',
            cell: ({ row }: { row: { original: UserRow } }) => {
              const user = row.original;
              return h('input', {
                type: 'checkbox',
                checked: user.suspended,
                onChange: (event: Event) => {
                  const target = event.target as HTMLInputElement;
                  user.suspended = target.checked;
                  this.handleSuspend(event, user);
                },
              });
            },
          },
          {
            accessorKey: 'admin',
            header: 'Admin?',
            cell: ({ row }: { row: { original: UserRow } }) => {
              const user = row.original;
              return h('input', {
                type: 'checkbox',
                checked: user.admin,
                onChange: (event: Event) => {
                  const target = event.target as HTMLInputElement;
                  user.admin = target.checked;
                  this.handleAdmin(event, user);
                },
              });
            },
          },
          {
            accessorKey: 'repeaters',
            header: 'Repeater Count',
            cell: ({ row }: { row: { original: UserRow } }) => `${row.original.repeaters}`,
          },
        );
      }

      columns.push({
        accessorKey: 'created_at',
        header: 'Created',
        cell: ({ row }: { row: { original: UserRow } }) => {
          const user = row.original;
          return h('span', { title: this.absoluteTime(user.created_at) }, this.relativeTime(user.created_at));
        },
      });

      return columns;
    },
    totalPages() {
      if (!this.totalRecords || this.totalRecords <= 0) {
        return 1;
      }
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
    ...mapStores(useUserStore, useSettingsStore),
  },
};
</script>

<style scoped></style>
