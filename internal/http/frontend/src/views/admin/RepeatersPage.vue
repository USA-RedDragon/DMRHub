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
  <div>
    <PVToast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Repeaters</template>
      <template #content>
        <RepeaterTable admin />
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';

import RepeaterTable from '@/components/RepeaterTable.vue';

export default {
  components: {
    Card,
    RepeaterTable,
  },
  head: {
    title: 'Repeaters',
    titleTemplate: 'Admin | %s | ' + (localStorage.getItem('title') || 'DMRHub'),
  },
  created() {},
  mounted() {},
  unmounted() {},
  data: function() {
    return {};
  },
  methods: {},
  computed: {},
};
</script>

<style scoped></style>
