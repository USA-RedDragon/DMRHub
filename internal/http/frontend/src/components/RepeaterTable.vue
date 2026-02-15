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
    <div class="table-header-container" v-if="!admin">
      <ShadButton as-child variant="outline" size="sm" class="no-underline">
        <RouterLink to="/repeaters/new">Enroll New Repeater</RouterLink>
      </ShadButton>
    </div>

    <DataTable
      :columns="columns"
      :data="repeaters"
      :loading="loading"
      :loading-text="'Loading...'"
      :empty-text="'No repeaters found'"
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
import { format, formatDistanceToNowStrict } from 'date-fns';
import { Button as ShadButton } from '@/components/ui/button';
import { DataTable } from '@/components/ui/data-table';

import API from '@/services/API';
import { getWebsocketURI } from '@/services/util';
import ws from '@/services/ws';

type Talkgroup = {
  id: number;
  name: string;
  display?: string;
};

type RepeaterRow = {
  id: number;
  type?: string;
  connected_time: string | Date;
  created_at: string | Date;
  last_ping_time: string | Date;
  slots: number;
  ts1_static_talkgroups: Talkgroup[];
  ts2_static_talkgroups: Talkgroup[];
  ts1_dynamic_talkgroup: Talkgroup;
  ts2_dynamic_talkgroup: Talkgroup;
  hotspot: boolean;
  simplex_repeater: boolean;
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
      talkgroups: [] as Talkgroup[],
      repeaters: [] as RepeaterRow[],
      socket: null as { close(): void } | null,
      editableRepeaters: 0,
      totalRecords: 0,
      page: 1,
      rows: 30,
      loading: false,
      now: Date.now(),
      timeInterval: null as ReturnType<typeof setInterval> | null,
    };
  },
  mounted() {
    this.fetchData();
    if (!this.admin) {
      this.socket = ws.connect(getWebsocketURI() + '/repeaters', this.onWebsocketMessage);
    }
    this.timeInterval = setInterval(() => { this.now = Date.now(); }, 30000);
  },
  unmounted() {
    if (this.timeInterval !== null) {
      clearInterval(this.timeInterval);
    }
    if (this.socket) {
      this.socket.close();
    }
  },
  computed: {
    columns() {
      // Reference this.now to trigger recomputation when the timer updates
      void this.now;
      const columns: ColumnDef<RepeaterRow, unknown>[] = [
        {
          accessorKey: 'row_actions',
          header: '',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const actionButton = (label: string, onClick: () => void) => {
              return h(ShadButton, {
                variant: 'outline',
                size: 'sm',
                onClick,
              }, label);
            };

            const repeater = row.original;
            if (!repeater.editable) {
              return actionButton('Edit', () => this.startEdit(repeater));
            }

            const actions: Array<ReturnType<typeof h>> = [
              actionButton('Save', () => this.saveTalkgroups(repeater)),
            ];
            actions.push(actionButton('Cancel', () => this.cancelEdit(repeater)));
            actions.push(actionButton('Delete', () => this.deleteRepeater(repeater)));

            return h('div', { class: 'flex gap-2 whitespace-nowrap' }, actions);
          },
        },
        {
          accessorKey: 'id',
          header: 'DMR Radio ID',
          cell: ({ row }: { row: { original: RepeaterRow } }) => `${row.original.id}`,
        },
        {
          accessorKey: 'type',
          header: 'Type',
          cell: ({ row }: { row: { original: RepeaterRow } }) => (row.original.type || 'mmdvm').toUpperCase(),
        },
        {
          accessorKey: 'connected_time',
          header: 'Last Connected',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            return this.hasTimestamp(repeater.connected_time) ? this.relativeTime(repeater.connected_time) : 'Never';
          },
        },
        {
          accessorKey: 'last_ping_time',
          header: 'Last Ping',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            return this.hasTimestamp(repeater.last_ping_time) ? this.relativeTime(repeater.last_ping_time) : 'Never';
          },
        },
        {
          accessorKey: 'ts1_static_talkgroups',
          header: 'TS1 Static TGs',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            if (!repeater.editable) {
              const options = repeater.slots === 1 || repeater.ts1_static_talkgroups.length === 0
                ? [{ id: 0, label: 'None' }]
                : repeater.ts1_static_talkgroups.map((tg) => ({ id: tg.id, label: `${tg.id} - ${tg.name}` }));
              return h(
                'select',
                {
                  id: 'ts1_static_talkgroups',
                  class: 'ui-select-multiple',
                  multiple: true,
                  disabled: true,
                },
                options.map((option) =>
                  h('option', { value: option.id }, option.label),
                ),
              );
            }

            if (repeater.slots === 1) {
              return '';
            }

            return h(
              'select',
              {
                id: 'ts1_static_talkgroups',
                class: 'ui-select-multiple',
                multiple: true,
                onChange: (event: Event) => {
                  const target = event.target as HTMLSelectElement;
                  const selected = Array.from(target.selectedOptions).map((option) => Number(option.value));
                  repeater.ts1_static_talkgroups = this.talkgroups.filter((tg) => selected.includes(tg.id));
                },
              },
              this.talkgroups.map((tg) =>
                h(
                  'option',
                  {
                    value: tg.id,
                    selected: repeater.ts1_static_talkgroups.some((selectedTG) => selectedTG.id === tg.id),
                  },
                  tg.display,
                ),
              ),
            );
          },
        },
        {
          accessorKey: 'ts2_static_talkgroups',
          header: 'TS2 Static TGs',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            if (!repeater.editable) {
              const options = repeater.ts2_static_talkgroups.length === 0
                ? [{ id: 0, label: 'None' }]
                : repeater.ts2_static_talkgroups.map((tg) => ({ id: tg.id, label: `${tg.id} - ${tg.name}` }));
              return h(
                'select',
                {
                  id: 'ts2_static_talkgroups',
                  class: 'ui-select-multiple',
                  multiple: true,
                  disabled: true,
                },
                options.map((option) =>
                  h('option', { value: option.id }, option.label),
                ),
              );
            }

            return h(
              'select',
              {
                id: 'ts2_static_talkgroups',
                class: 'ui-select-multiple',
                multiple: true,
                onChange: (event: Event) => {
                  const target = event.target as HTMLSelectElement;
                  const selected = Array.from(target.selectedOptions).map((option) => Number(option.value));
                  repeater.ts2_static_talkgroups = this.talkgroups.filter((tg) => selected.includes(tg.id));
                },
              },
              this.talkgroups.map((tg) =>
                h(
                  'option',
                  {
                    value: tg.id,
                    selected: repeater.ts2_static_talkgroups.some((selectedTG) => selectedTG.id === tg.id),
                  },
                  tg.display,
                ),
              ),
            );
          },
        },
        {
          accessorKey: 'ts1_dynamic_talkgroup',
          header: 'TS1 Dynamic TG',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            if (!repeater.editable) {
              const hasDynamic = repeater.slots !== 1 && repeater.ts1_dynamic_talkgroup.id !== 0;
              const options = hasDynamic
                ? [{ id: repeater.ts1_dynamic_talkgroup.id, label: `${repeater.ts1_dynamic_talkgroup.id} - ${repeater.ts1_dynamic_talkgroup.name}` }]
                : [{ id: 0, label: 'None' }];
              return h(
                'select',
                {
                  id: 'ts1_dynamic_talkgroup',
                  class: 'ui-select',
                  disabled: true,
                  value: options[0]?.id ?? 0,
                },
                options.map((option) => h('option', { value: option.id }, option.label)),
              );
            }

            if (repeater.slots === 1) {
              return '';
            }

            const options = [{ id: 0, name: 'None', display: '0 - None' }, ...this.talkgroups];
            return h(
              'select',
              {
                id: 'ts1_dynamic_talkgroup',
                class: 'ui-select',
                value: repeater.ts1_dynamic_talkgroup.id,
                onChange: (event: Event) => {
                  const target = event.target as HTMLSelectElement;
                  const selectedID = Number(target.value);
                  const selected = options.find((tg) => tg.id === selectedID);
                  repeater.ts1_dynamic_talkgroup = selected || { id: 0, name: 'None', display: '0 - None' };
                },
              },
              options.map((tg) => h('option', { value: tg.id }, tg.display)),
            );
          },
        },
        {
          accessorKey: 'ts2_dynamic_talkgroup',
          header: 'TS2 Dynamic TG',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            if (!repeater.editable) {
              const hasDynamic = repeater.ts2_dynamic_talkgroup.id !== 0;
              const options = hasDynamic
                ? [{ id: repeater.ts2_dynamic_talkgroup.id, label: `${repeater.ts2_dynamic_talkgroup.id} - ${repeater.ts2_dynamic_talkgroup.name}` }]
                : [{ id: 0, label: 'None' }];
              return h(
                'select',
                {
                  id: 'ts2_dynamic_talkgroup',
                  class: 'ui-select',
                  disabled: true,
                  value: options[0]?.id ?? 0,
                },
                options.map((option) => h('option', { value: option.id }, option.label)),
              );
            }

            const options = [{ id: 0, name: 'None', display: '0 - None' }, ...this.talkgroups];
            return h(
              'select',
              {
                id: 'ts2_dynamic_talkgroup',
                class: 'ui-select',
                value: repeater.ts2_dynamic_talkgroup.id,
                onChange: (event: Event) => {
                  const target = event.target as HTMLSelectElement;
                  const selectedID = Number(target.value);
                  const selected = options.find((tg) => tg.id === selectedID);
                  repeater.ts2_dynamic_talkgroup = selected || { id: 0, name: 'None', display: '0 - None' };
                },
              },
              options.map((tg) => h('option', { value: tg.id }, tg.display)),
            );
          },
        },
        {
          accessorKey: 'hotspot',
          header: 'Hotspot',
          cell: ({ row }: { row: { original: RepeaterRow } }) => `${row.original.hotspot}`,
        },
        {
          accessorKey: 'simplex_repeater',
          header: 'Simplex',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            if (!repeater.editable) {
              return `${repeater.simplex_repeater}`;
            }
            return h('input', {
              type: 'checkbox',
              checked: repeater.simplex_repeater,
              onChange: (e: Event) => {
                repeater.simplex_repeater = (e.target as HTMLInputElement).checked;
              },
            });
          },
        },
        {
          accessorKey: 'created_at',
          header: 'Created',
          cell: ({ row }: { row: { original: RepeaterRow } }) => {
            const repeater = row.original;
            return h('span', { title: this.absoluteTime(repeater.created_at) }, this.relativeTime(repeater.created_at));
          },
        },
      ];

      return columns;
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
      API.get('/talkgroups?limit=none')
        .then((res) => {
          this.talkgroups = res.data.talkgroups;
          let parrotIndex = -1;
          for (let i = 0; i < this.talkgroups.length; i++) {
            const tg = this.talkgroups[i];
            if (!tg) continue;
            tg.display = tg.id + ' - ' + tg.name;

            if (tg.id == 9990) {
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

      if (this.editableRepeaters === 0) {
        if (this.admin) {
          API.get(`/repeaters?limit=${limit}&page=${page}`)
            .then((res) => {
              this.repeaters = this.cleanData(res.data.repeaters);
              this.totalRecords = res.data.total;
              this.loading = false;
            })
            .catch((err) => {
              console.error(err);
              this.loading = false;
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
              this.loading = false;
            });
        }
      }
    },
    cleanData(data: RepeaterRow[]) {
      const copyData: RepeaterRow[] = JSON.parse(JSON.stringify(data));
      for (const repeater of copyData) {
        repeater.connected_time = new Date(repeater.connected_time);
        repeater.created_at = new Date(repeater.created_at);
        repeater.last_ping_time = new Date(repeater.last_ping_time);
        repeater.editable = false;

        for (const tg of repeater.ts1_static_talkgroups) {
          tg.display = `${tg.id} - ${tg.name}`;
        }

        for (const tg of repeater.ts2_static_talkgroups) {
          tg.display = `${tg.id} - ${tg.name}`;
        }

        repeater.ts1_dynamic_talkgroup.display =
          `${repeater.ts1_dynamic_talkgroup.id} - ${repeater.ts1_dynamic_talkgroup.name}`;

        repeater.ts2_dynamic_talkgroup.display =
          `${repeater.ts2_dynamic_talkgroup.id} - ${repeater.ts2_dynamic_talkgroup.name}`;
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
      return formatDistanceToNowStrict(date, { addSuffix: true });
    },
    absoluteTime(dateValue: string | Date) {
      const date = new Date(dateValue);
      if (Number.isNaN(date.getTime())) {
        return '-';
      }
      return format(date, 'yyyy-MM-dd HH:mm:ss');
    },
    startEdit(repeater: RepeaterRow) {
      repeater.editable = true;
      this.editableRepeaters++;
    },
    cancelEdit(repeater: RepeaterRow) {
      repeater.editable = false;
      this.editableRepeaters--;
      if (this.editableRepeaters == 0) {
        this.fetchData();
      }
    },
    saveTalkgroups(repeater: RepeaterRow) {
      const talkgroupsPromise = API.post(`/repeaters/${repeater.id}/talkgroups`, {
        ts1_dynamic_talkgroup: repeater.ts1_dynamic_talkgroup,
        ts2_dynamic_talkgroup: repeater.ts2_dynamic_talkgroup,
        ts1_static_talkgroups: repeater.ts1_static_talkgroups,
        ts2_static_talkgroups: repeater.ts2_static_talkgroups,
      });
      const settingsPromise = API.patch(`/repeaters/${repeater.id}`, {
        simplex_repeater: repeater.simplex_repeater,
      });
      Promise.all([talkgroupsPromise, settingsPromise])
        .then(() => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: `Repeater ${repeater.id} updated`,
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
              detail: `Error updating repeater ${repeater.id}`,
              life: 3000,
            });
          }
        });
    },
    unlink(ts: number, repeater: RepeaterRow) {
      // API call: POST /repeaters/:id/unlink/dynamic/:ts/:tg
      let tg = 0;
      if (ts == 1) {
        const ts1 = repeater['ts1_dynamic_talkgroup'] as { id: number } | undefined;
        tg = ts1?.id ?? 0;
      } else if (ts == 2) {
        const ts2 = repeater['ts2_dynamic_talkgroup'] as { id: number } | undefined;
        tg = ts2?.id ?? 0;
      }
      API.post(`/repeaters/${repeater.id}/unlink/dynamic/${ts}/${tg}`, {})
        .then(() => {
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
    deleteRepeater(repeater: RepeaterRow) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: 'Are you sure you want to delete this repeater?',
        header: 'Delete Repeater',
        icon: 'pi pi-exclamation-triangle',
        acceptClass: 'p-button-danger',
        accept: () => {
          API.delete('/repeaters/' + repeater.id)
            .then(() => {
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
    onWebsocketMessage() {
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

.ui-select,
.ui-select-multiple {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}

.ui-select-multiple {
  min-height: 6rem;
}
</style>
