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
  <div>
    <AppHeader />
    <RouterView />
    <AppFooter />
    <Toaster />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';
import { RouterView } from 'vue-router';
import { useHead } from '@unhead/vue';
import { Toaster } from '@/components/ui/toast';
import AppFooter from '@/components/AppFooter.vue';
import AppHeader from '@/components/AppHeader.vue';
import API from '@/services/API';
import { useUserStore } from '@/store';

const userStore = useUserStore();
let refresh = 0;

useHead({
  titleTemplate: `%s | ${localStorage.getItem('title') || 'DMRHub'}`,
  meta: [
    {
      name: 'description',
      content: 'DMRHub is a DMR network server like TGIF or BrandMeister ran in a single binary.',
    },
  ],
});

const fetchData = () => {
  API.get('/users/me')
    .then((res) => {
      userStore.id = res.data.id;
      userStore.callsign = res.data.callsign;
      userStore.username = res.data.username;
      userStore.admin = res.data.admin;
      userStore.superAdmin = res.data.superAdmin;
      userStore.created_at = res.data.created_at;
      userStore.loggedIn = true;
    })
    .catch(() => {
      userStore.loggedIn = false;
    });
};

onMounted(() => {
  fetchData();
  refresh = window.setInterval(fetchData, 5000);
});

onUnmounted(() => {
  if (refresh !== 0) {
    clearInterval(refresh);
  }
});
</script>

<style scoped></style>
