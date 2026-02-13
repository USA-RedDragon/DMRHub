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
  <header>
    <h1>
      <router-link to="/">{{ title }}</router-link>
    </h1>
    <div class="wrapper">
      <nav>
        <router-link to="/">Home</router-link>
        <router-link to="/lastheard">Last Heard</router-link>
        <router-link v-if="userStore.loggedIn" to="/repeaters">Repeaters</router-link>
        <div
          v-if="userStore.loggedIn"
          class="nav-dropdown"
          :class="{
            activeDropdown: route.path.startsWith('/talkgroups'),
          }"
          @mouseenter="setMenuOpen('talkgroups', true)"
          @mouseleave="scheduleCloseMenus"
        >
          <DropdownMenuRoot
            :open="openTalkgroupsMenu"
            :modal="false"
            @update:open="setMenuOpen('talkgroups', $event)"
          >
            <DropdownMenuTrigger as-child>
              <button class="nav-trigger" type="button">Talkgroups</button>
            </DropdownMenuTrigger>
            <DropdownMenuContent class="dropdown-content" :side-offset="0" align="start">
              <DropdownMenuItem as-child>
                <router-link to="/talkgroups" @click="closeMenus">List</router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link to="/talkgroups/owned" @click="closeMenus">Owned</router-link>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuRoot>
        </div>

        <router-link v-if="openBridgeFeature" to="/peers">OpenBridge Peers</router-link>
        <div
          v-if="userStore.loggedIn && userStore.admin"
          class="nav-dropdown"
          :class="{
            activeDropdown: route.path.startsWith('/admin'),
          }"
          @mouseenter="setMenuOpen('admin', true)"
          @mouseleave="scheduleCloseMenus"
        >
          <DropdownMenuRoot
            :open="openAdminMenu"
            :modal="false"
            @update:open="setMenuOpen('admin', $event)"
          >
            <DropdownMenuTrigger as-child>
              <button class="nav-trigger" type="button">Admin</button>
            </DropdownMenuTrigger>
            <DropdownMenuContent class="dropdown-content" :side-offset="0" align="start">
              <DropdownMenuItem as-child>
                <router-link to="/admin/talkgroups" @click="closeMenus">Talkgroups</router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link to="/admin/repeaters" @click="closeMenus">Repeaters</router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link to="/admin/users" @click="closeMenus">Users</router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link to="/admin/users/approval" @click="closeMenus">User Approvals</router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link to="/admin/setup" @click="closeMenus">Setup</router-link>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuRoot>
        </div>
        <router-link v-if="!userStore.loggedIn" to="/register"
          >Register</router-link
        >
        <router-link v-if="!userStore.loggedIn" to="/login"
          >Login</router-link
        >
        <a v-else href="#" @click="logout()">Logout</a>
      </nav>
    </div>
  </header>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from 'reka-ui';
import API from '@/services/API';
import { useUserStore } from '@/store';

const userStore = useUserStore();
const route = useRoute();
const router = useRouter();

const title = ref(localStorage.getItem('title') || 'DMRHub');
const openBridgeFeature = ref(false);
const openTalkgroupsMenu = ref(false);
const openAdminMenu = ref(false);
let closeMenuTimer = 0;

const setTitle = (newTitle: string) => {
  localStorage.setItem('title', newTitle);
  title.value = newTitle;
};

const getTitle = () => {
  API.get('/network/name')
    .then((response) => {
      setTitle(response.data);
    })
    .catch((error) => {
      console.log(error);
    });
};

const clearCloseMenuTimer = () => {
  if (closeMenuTimer !== 0) {
    clearTimeout(closeMenuTimer);
    closeMenuTimer = 0;
  }
};

const closeMenus = () => {
  clearCloseMenuTimer();
  openTalkgroupsMenu.value = false;
  openAdminMenu.value = false;
};

const setMenuOpen = (menu: 'talkgroups' | 'admin', isOpen: boolean) => {
  clearCloseMenuTimer();

  if (menu === 'talkgroups') {
    openTalkgroupsMenu.value = isOpen;
    if (isOpen) {
      openAdminMenu.value = false;
    }
  } else {
    openAdminMenu.value = isOpen;
    if (isOpen) {
      openTalkgroupsMenu.value = false;
    }
  }
};

const scheduleCloseMenus = () => {
  clearCloseMenuTimer();
  closeMenuTimer = window.setTimeout(() => {
    closeMenus();
  }, 150);
};

const handleOutsideClick = (event: MouseEvent) => {
  const path = event.composedPath();
  const clickedInHeader = path.some((node: EventTarget) => {
    return node instanceof HTMLElement && node.tagName === 'HEADER';
  });

  if (!clickedInHeader) {
    closeMenus();
  }
};

const logout = () => {
  API.get('/auth/logout')
    .then(() => {
      closeMenus();
      userStore.loggedIn = false;
      router.push('/login');
    })
    .catch((err) => {
      console.error(err);
    });
};

onMounted(() => {
  getTitle();
  document.addEventListener('click', handleOutsideClick);
});

onUnmounted(() => {
  clearCloseMenuTimer();
  document.removeEventListener('click', handleOutsideClick);
});
</script>

<style scoped>
header {
  text-align: center;
  max-height: 100vh;
}

header a,
header a:visited,
header a:link,
.adminNavLink:visited,
.adminNavLink:link {
  color: var(--primary-text-color);
}

header nav a,
.adminNavLink {
  text-decoration: underline;
  text-decoration-color: transparent;
  text-underline-offset: 0.25rem;
  font-weight: 600;
  transition: color 0.2s ease, text-decoration-color 0.2s ease;
}

header nav a:hover,
.adminNavLink:hover {
  color: var(--cyan-300) !important;
  text-decoration-color: currentColor;
}

header nav .router-link-active,
.adminNavLink.router-link-active {
  color: var(--cyan-300) !important;
  text-decoration-color: currentColor;
}

header nav a:active,
.adminNavLink:active {
  color: var(--cyan-500) !important;
}

header h1 a,
header h1 a:hover,
header h1 a:visited {
  color: var(--primary-text-color);
  background-color: inherit;
}

nav {
  width: 100%;
  font-size: 1rem;
  text-align: center;
  margin-top: 0.5rem;
  margin-bottom: 1rem;
  display: flex;
  justify-content: center;
  align-items: center;
  flex-wrap: wrap;
}

nav a {
  display: inline-block;
  padding: 0 1rem;
  border-left: 2px solid #444;
}

nav a:first-of-type {
  border: 0;
}

.nav-dropdown {
  display: inline-block;
  padding: 0 1rem;
  border-left: 2px solid #444;
  position: relative;
}

.nav-trigger {
  background: transparent;
  border: 0;
  padding: 0;
  font: inherit;
  cursor: pointer;
  color: var(--primary-text-color);
  text-decoration: underline;
  text-decoration-color: transparent;
  text-underline-offset: 0.25rem;
  font-weight: 600;
}

.nav-dropdown:hover .nav-trigger,
.activeDropdown .nav-trigger {
  color: var(--cyan-300);
  text-decoration-color: currentColor;
}

:deep(.dropdown-content) {
  min-width: 11rem !important;
  background: var(--background);
  color: var(--foreground);
  border: 1px solid var(--border);
  border-radius: 0.35rem;
  z-index: 10;
  padding: 0.25rem 0;
}

:deep(.dropdown-content a) {
  display: block;
  border-left: 0;
  padding: 0.4rem 0.75rem;
  text-align: left;
  text-decoration: none;
  color: var(--foreground);
}

:deep(.dropdown-content a:hover) {
  background: var(--border);
}
</style>
