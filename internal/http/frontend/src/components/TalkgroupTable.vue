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
  <div class="space-y-4">
    <div class="table-header-container" v-if="admin">
      <ShadButton as-child variant="outline" size="sm" class="no-underline">
        <RouterLink to="/admin/talkgroups/new">Add New Talkgroup</RouterLink>
      </ShadButton>
    </div>

    <DataTable
      :columns="columns"
      :data="talkgroups"
      :loading="loading"
      :loading-text="'Loading...'"
      :empty-text="'No talkgroups found'"
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
import { h, type RendererElement } from 'vue';
import { Button as ShadButton } from '@/components/ui/button';
import { buttonVariants } from '@/components/ui/button';
import { Input as Input } from '@/components/ui/input';
import { DataTable } from '@/components/ui/data-table';

import { format, formatDistanceToNowStrict } from 'date-fns';

import { mapStores } from 'pinia';
import { useSettingsStore } from '@/store';

import API from '@/services/API';

type TalkgroupUser = { id: number; callsign: string; display?: string };
type TalkgroupRow = {
  id: number;
  name: string;
  description: string;
  admins: TalkgroupUser[];
  ncos: TalkgroupUser[];
  created_at: string | Date;
  editable: boolean;
};

export default {
  name: 'TalkgroupTable',
  props: {
    admin: Boolean,
    owner: Boolean,
  },
  components: {
    ShadButton,
    DataTable,
  },
  data: function() {
    return {
      talkgroups: [] as TalkgroupRow[],
      editableTalkgroups: 0,
      totalRecords: 0,
      page: 1,
      rows: 30,
      loading: false,
      allUsers: [] as TalkgroupUser[],
      now: Date.now(),
      timeInterval: null as ReturnType<typeof setInterval> | null,
    };
  },
  mounted() {
    this.fetchData();
    this.timeInterval = setInterval(() => { this.now = Date.now(); }, 30000);
  },
  unmounted() {
    if (this.timeInterval !== null) {
      clearInterval(this.timeInterval);
    }
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
            this.loading = false;
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
            this.loading = false;
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
            this.loading = false;
          });
      }
    },
    cleanData(data: TalkgroupRow[]) {
      const copyData: TalkgroupRow[] = JSON.parse(JSON.stringify(data));

      for (const talkgroup of copyData) {
        talkgroup.created_at = new Date(talkgroup.created_at);
        talkgroup.editable = false;
        talkgroup.admins = talkgroup.admins || [];
        talkgroup.ncos = talkgroup.ncos || [];

        for (const adminUser of talkgroup.admins) {
          adminUser.display = `${adminUser.id} - ${adminUser.callsign}`;
        }

        for (const nco of talkgroup.ncos) {
          nco.display = `${nco.id} - ${nco.callsign}`;
        }
      }
      return copyData;
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
    startEdit(talkgroup: TalkgroupRow) {
      this.editableTalkgroups++;
      talkgroup.editable = true;
    },
    cancelEdit(talkgroup: TalkgroupRow) {
      talkgroup.editable = false;
      this.editableTalkgroups--;
      if (this.editableTalkgroups == 0) {
        this.fetchData();
      }
    },
    deleteTalkgroup(talkgroup: TalkgroupRow) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: 'Are you sure you want to delete this talkgroup?',
        header: 'Delete Talkgroup',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.delete('/talkgroups/' + talkgroup.id)
            .then(() => {
              this.$toast.add({
                summary: 'Confirmed',
                severity: 'success',
                detail: `Talkgroup ${talkgroup.id} deleted`,
                life: 3000,
              });
              // Immediately remove the deleted talkgroup from the local array
              this.talkgroups = this.talkgroups.filter(t => t.id !== talkgroup.id);
              this.totalRecords = Math.max(0, this.totalRecords - 1);
              talkgroup.editable = false;
              this.editableTalkgroups--;
              if (this.editableTalkgroups === 0) {
                this.fetchData();
              }
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
    saveTalkgroup(talkgroup: TalkgroupRow) {
      const admins = talkgroup.admins;
      const ncos = talkgroup.ncos;
      API.patch('/talkgroups/' + talkgroup.id, {
        name: talkgroup.name,
        description: talkgroup.description,
      })
        .then(() => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: 'Talkgroup updated',
            life: 3000,
          });
          API.post(`/talkgroups/${talkgroup.id}/admins`, {
            user_ids: admins ? admins.map((admin) => admin.id) : [],
          })
            .then(() => {
              API.post(`/talkgroups/${talkgroup.id}/ncos`, {
                user_ids: ncos ? ncos.map((nco) => nco.id) : [],
              })
                .then(() => {
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
    columns() {
      // Reference this.now to trigger recomputation when the timer updates
      void this.now;
      const columns: ColumnDef<TalkgroupRow, RendererElement>[] = [];

      if (this.admin || this.owner) {
        columns.push({
          accessorKey: 'actions',
          header: '',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => {
            const talkgroup = row.original;
            const actions: Array<ReturnType<typeof h>> = [];
            const actionButton = (label: string, onClick: () => void) => {
              return h('button', {
                class: buttonVariants({ variant: 'outline', size: 'sm' }),
                onClick,
              }, label);
            };
            if (!talkgroup.editable) {
              actions.push(actionButton('Edit', () => this.startEdit(talkgroup)));
            }
            if (talkgroup.editable) {
              actions.push(actionButton('Save', () => this.saveTalkgroup(talkgroup)));
              actions.push(actionButton('Cancel', () => this.cancelEdit(talkgroup)));
            }
            if (this.admin && !talkgroup.editable) {
              actions.push(actionButton('Delete', () => this.deleteTalkgroup(talkgroup)));
            }
            return h('div', { class: 'flex gap-2' }, actions);
          },
        });
      }

      columns.push(
        {
          accessorKey: 'id',
          header: 'Channel',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => `${row.original.id}`,
        },
        {
          accessorKey: 'name',
          header: 'Name',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => {
            const talkgroup = row.original;
            if (!talkgroup.editable) {
              return talkgroup.name;
            }
            return h(Input, {
              modelValue: talkgroup.name,
              'onUpdate:modelValue': (value: string | number) => {
                talkgroup.name = String(value);
              },
            });
          },
        },
        {
          accessorKey: 'description',
          header: 'Description',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => {
            const talkgroup = row.original;
            if (!talkgroup.editable) {
              return talkgroup.description;
            }
            return h(Input, {
              modelValue: talkgroup.description,
              'onUpdate:modelValue': (value: string | number) => {
                talkgroup.description = String(value);
              },
            });
          },
        },
      );

      if (!this.owner) {
        columns.push({
          accessorKey: 'admins',
          header: 'Admins',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => {
            const talkgroup = row.original;
            if (!talkgroup.editable) {
              if (talkgroup.admins.length === 0) {
                return 'None';
              }
              return talkgroup.admins.map((adminUser) => adminUser.display).join(' ');
            }

            return h(
              'select',
              {
                id: 'admins',
                class: 'ui-select-multiple',
                multiple: true,
                onChange: (event: Event) => {
                  const target = event.target as HTMLSelectElement;
                  const selected = Array.from(target.selectedOptions).map((option) => Number(option.value));
                  talkgroup.admins = this.allUsers.filter((user) => selected.includes(user.id));
                },
              },
              this.allUsers.map((user) =>
                h(
                  'option',
                  {
                    value: user.id,
                    selected: talkgroup.admins.some((adminUser) => adminUser.id === user.id),
                  },
                  user.display,
                ),
              ),
            );
          },
        });
      }

      columns.push(
        {
          accessorKey: 'ncos',
          header: 'Net Control Operators',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => {
            const talkgroup = row.original;
            if (!talkgroup.editable) {
              if (!talkgroup.ncos || talkgroup.ncos.length === 0) {
                return 'None';
              }
              return talkgroup.ncos.map((nco) => nco.display).join(' ');
            }

            return h(
              'select',
              {
                id: 'ncos',
                class: 'ui-select-multiple',
                multiple: true,
                onChange: (event: Event) => {
                  const target = event.target as HTMLSelectElement;
                  const selected = Array.from(target.selectedOptions).map((option) => Number(option.value));
                  talkgroup.ncos = this.allUsers.filter((user) => selected.includes(user.id));
                },
              },
              this.allUsers.map((user) =>
                h(
                  'option',
                  {
                    value: user.id,
                    selected: talkgroup.ncos.some((nco) => nco.id === user.id),
                  },
                  user.display,
                ),
              ),
            );
          },
        },
        {
          accessorKey: 'created_at',
          header: 'Created',
          cell: ({ row }: { row: { original: TalkgroupRow } }) => {
            const talkgroup = row.original;
            return h(
              'span',
              { title: this.absoluteTime(talkgroup.created_at) },
              this.relativeTime(talkgroup.created_at),
            );
          },
        },
      );

      return columns;
    },
    totalPages() {
      if (!this.totalRecords || this.totalRecords <= 0) {
        return 1;
      }
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
    ...mapStores(useSettingsStore),
  },
};
</script>

<style scoped>
.table-header-container {
  display: flex;
  justify-content: flex-end;
}

.ui-select-multiple {
  width: 100%;
  min-height: 6rem;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}
</style>
