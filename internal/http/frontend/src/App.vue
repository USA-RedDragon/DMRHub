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
  <div class="min-h-screen flex flex-col">
    <AppHeader />
    <main class="flex-1 mx-auto w-full max-w-9.5/10 px-4 sm:px-6 lg:px-8 py-6">
      <RouterView />
    </main>
    <AppFooter />
    <Toaster />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue';
import { RouterView } from 'vue-router';
import { useHead } from '@unhead/vue';
import { Toaster } from '@/components/ui/toast';
import AppFooter from '@/components/AppFooter.vue';
import AppHeader from '@/components/AppHeader.vue';
import API from '@/services/API';
import { useRoute, useRouter } from 'vue-router';
import { useSettingsStore, useUserStore } from '@/store';

const userStore = useUserStore();
const settingsStore = useSettingsStore();
const route = useRoute();
const router = useRouter();
const authPages = ['/login', '/register', '/forgot-password', '/reset-password', '/verify-email', '/oauth/authorize'];

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
      userStore.mustChangePassword = res.data.must_change_password === true;
      userStore.loggedIn = true;
    })
    .catch(async () => {
      userStore.loggedIn = false;
      await settingsStore.fetchConfig();
      if (settingsStore.visibility === 'login' && !authPages.includes(route.path)) {
        await router.push('/login');
      }
    });
};

onMounted(() => {
  fetchData();
});
</script>

<style scoped></style>
