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
  <div class="flex flex-col gap-4">
    <TypeFilter v-model="filter" />
    <DataTable
      :columns="columns"
      :data="lastheard"
      :loading="loading"
      :loading-text="'Loading...'"
      :empty-text="'No transmissions match these types'"
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
import { DataTable } from '@/components/ui/data-table';
import TypeFilter from '@/components/TypeFilter.vue';
import { useUserStore } from '@/store';

import { getWebsocketURI } from '@/services/util';
import API from '@/services/API';
import ws from '@/services/ws';
import { lastHeardColumns } from '@/lib/lastHeardColumns';
import {
  cleanCalls,
  defaultFilter,
  filterQuery,
  matchesFilter,
  type CallRow,
  type TypeFilterValue,
} from '@/lib/transmissions';

export default {
  name: 'LastHeardTable',
  props: {},
  components: {
    DataTable,
    TypeFilter,
  },
  data: function() {
    return {
      lastheard: [] as CallRow[],
      totalRecords: 0,
      page: 1,
      rows: 30,
      filter: defaultFilter() as TypeFilterValue,
      socket: null as { close(): void } | null,
      loading: false,
      now: Date.now(),
      timeInterval: null as ReturnType<typeof setInterval> | null,
    };
  },
  mounted() {
    this.fetchData();
    this.connectSocket();
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
  watch: {
    loggedIn() {
      this.connectSocket();
    },
    filter: {
      deep: true,
      handler() {
        this.page = 1;
        this.fetchData(this.page, this.rows);
      },
    },
  },
  computed: {
    columns() {
      void this.now;
      return lastHeardColumns({ repeater: this.loggedIn });
    },
    loggedIn(): boolean {
      const userStore = useUserStore();
      return userStore.loggedIn;
    },
    totalPages() {
      if (!this.totalRecords || this.totalRecords <= 0) {
        return 1;
      }
      return Math.max(1, Math.ceil(this.totalRecords / this.rows));
    },
  },
  methods: {
    connectSocket() {
      if (this.socket) {
        this.socket.close();
      }
      const path = this.loggedIn ? '/calls' : '/lastheard';
      this.socket = ws.connect(getWebsocketURI() + path, this.onWebsocketMessage);
    },
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
      if (this.filter.types.length === 0) {
        this.lastheard = [];
        this.totalRecords = 0;
        return;
      }
      this.loading = true;
      API.get(`/lastheard?page=${page}&limit=${limit}&${filterQuery(this.filter)}`)
        .then((res) => {
          this.totalRecords = res.data.total;
          this.lastheard = cleanCalls(res.data.calls);
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
          this.loading = false;
        });
    },
    onWebsocketMessage(event: MessageEvent) {
      const call = JSON.parse(event.data);
      const copyLastheard = JSON.parse(JSON.stringify(this.lastheard));
      let found = false;

      for (let i = 0; i < copyLastheard.length; i++) {
        if (copyLastheard[i].id == call.id) {
          found = true;
          copyLastheard[i] = call;
          break;
        }
      }

      if (!found && !matchesFilter(call, this.filter)) {
        return;
      }

      if (!found && copyLastheard.length === this.rows) {
        copyLastheard.pop();
      }

      if (!found && copyLastheard.length < this.rows) {
        copyLastheard.unshift(call);
      }

      this.lastheard = cleanCalls(copyLastheard);
    },
  },
};
</script>

<style scoped></style>
