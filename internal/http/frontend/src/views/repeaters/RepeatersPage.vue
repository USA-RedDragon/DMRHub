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
    <h1 id="repeaters-heading">Repeaters</h1>
    <div v-if="loggedIn" class="flex flex-wrap items-center justify-between gap-2">
      <SegmentPicker
        name="repeaters-view"
        labelled-by="repeaters-heading"
        :options="views"
        :model-value="view"
        @update:model-value="setView"
      />
      <ShadButton v-if="view === 'local'" as-child size="sm" class="no-underline" data-test="new-local-group">
        <RouterLink to="/localgroups/new">New local group</RouterLink>
      </ShadButton>
    </div>
    <CosysopInvites @accepted="reload += 1" />
    <LocalGroupTable v-if="view === 'local'" />
    <RepeaterTable v-else :key="reload" />
  </div>
</template>

<script lang="ts">
import CosysopInvites from '@/components/CosysopInvites.vue';
import { RouterLink } from 'vue-router';
import { Button as ShadButton } from '@/components/ui/button';
import LocalGroupTable from '@/components/LocalGroupTable.vue';
import RepeaterTable from '@/components/RepeaterTable.vue';
import SegmentPicker from '@/components/SegmentPicker.vue';
import { useUserStore } from '@/store';

const VIEWS = [
  { value: 'mine', label: 'My repeaters' },
  { value: 'local', label: 'Local groups' },
];

export default {
  components: {
    CosysopInvites,
    LocalGroupTable,
    RepeaterTable,
    RouterLink,
    SegmentPicker,
    ShadButton,
  },
  head: {
    title: 'Repeaters',
  },
  data: function() {
    return { views: VIEWS, reload: 0 };
  },
  computed: {
    view(): string {
      return this.$route.query.view === 'local' ? 'local' : 'mine';
    },
    loggedIn(): boolean {
      return useUserStore().loggedIn;
    },
  },
  methods: {
    setView(v: string) {
      this.$router.replace({ query: v === 'local' ? { view: 'local' } : {} });
    },
  },
};
</script>

<style scoped></style>
