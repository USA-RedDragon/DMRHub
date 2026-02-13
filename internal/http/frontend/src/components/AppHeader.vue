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
  <!-- Desktop top bar -->
  <header
    class="sticky top-0 z-50 w-full border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60"
  >
    <div class="flex h-14 items-center px-2">
      <!-- Mobile hamburger -->
      <Sheet v-model:open="mobileOpen">
        <SheetTrigger as-child>
          <Button variant="ghost" size="icon" class="mr-2 md:hidden">
            <Menu class="h-5 w-5" />
            <span class="sr-only">Toggle menu</span>
          </Button>
        </SheetTrigger>
        <SheetContent side="left" class="w-72 p-0">
          <SheetHeader class="border-b border-border px-4 py-4">
            <SheetTitle>
              <router-link
                to="/"
                class="text-lg font-bold text-foreground no-underline"
                @click="mobileOpen = false"
              >
                {{ title }}
              </router-link>
            </SheetTitle>
            <SheetDescription class="sr-only">Navigation menu</SheetDescription>
          </SheetHeader>

          <nav class="flex flex-col gap-1 px-2 py-3">
            <router-link
              to="/"
              class="mobile-nav-link"
              :class="{ active: route.path === '/' }"
              @click="mobileOpen = false"
            >
              <Home class="h-4 w-4" />
              Home
            </router-link>
            <router-link
              to="/lastheard"
              class="mobile-nav-link"
              :class="{ active: route.path === '/lastheard' }"
              @click="mobileOpen = false"
            >
              <Radio class="h-4 w-4" />
              Last Heard
            </router-link>
            <router-link
              v-if="userStore.loggedIn"
              to="/repeaters"
              class="mobile-nav-link"
              :class="{ active: route.path === '/repeaters' }"
              @click="mobileOpen = false"
            >
              <Antenna class="h-4 w-4" />
              Repeaters
            </router-link>

            <!-- Talkgroups section -->
            <template v-if="userStore.loggedIn">
              <Separator class="my-1" />
              <span class="px-3 py-1 text-xs font-medium text-muted-foreground">Talkgroups</span>
              <router-link
                to="/talkgroups"
                class="mobile-nav-link"
                :class="{ active: route.path === '/talkgroups' }"
                @click="mobileOpen = false"
              >
                <MessageSquare class="h-4 w-4" />
                List
              </router-link>
              <router-link
                to="/talkgroups/owned"
                class="mobile-nav-link"
                :class="{ active: route.path === '/talkgroups/owned' }"
                @click="mobileOpen = false"
              >
                <MessageSquare class="h-4 w-4" />
                Owned
              </router-link>
            </template>

            <!-- OpenBridge Peers -->
            <template v-if="openBridgeFeature">
              <Separator class="my-1" />
              <router-link
                to="/peers"
                class="mobile-nav-link"
                :class="{ active: route.path === '/peers' }"
                @click="mobileOpen = false"
              >
                <Globe class="h-4 w-4" />
                OpenBridge Peers
              </router-link>
            </template>

            <!-- Admin section -->
            <template v-if="userStore.loggedIn && userStore.admin">
              <Separator class="my-1" />
              <span class="px-3 py-1 text-xs font-medium text-muted-foreground">Admin</span>
              <router-link
                to="/admin/talkgroups"
                class="mobile-nav-link"
                :class="{ active: route.path === '/admin/talkgroups' }"
                @click="mobileOpen = false"
              >
                <MessageSquare class="h-4 w-4" />
                Talkgroups
              </router-link>
              <router-link
                to="/admin/repeaters"
                class="mobile-nav-link"
                :class="{ active: route.path === '/admin/repeaters' }"
                @click="mobileOpen = false"
              >
                <Antenna class="h-4 w-4" />
                Repeaters
              </router-link>
              <router-link
                to="/admin/users"
                class="mobile-nav-link"
                :class="{ active: route.path === '/admin/users' }"
                @click="mobileOpen = false"
              >
                <Users class="h-4 w-4" />
                Users
              </router-link>
              <router-link
                to="/admin/users/approval"
                class="mobile-nav-link"
                :class="{ active: route.path === '/admin/users/approval' }"
                @click="mobileOpen = false"
              >
                <UserCheck class="h-4 w-4" />
                User Approvals
              </router-link>
              <router-link
                to="/admin/setup"
                class="mobile-nav-link"
                :class="{ active: route.path === '/admin/setup' }"
                @click="mobileOpen = false"
              >
                <Settings class="h-4 w-4" />
                Setup
              </router-link>
            </template>

            <!-- Auth -->
            <Separator class="my-1" />
            <template v-if="!userStore.loggedIn">
              <router-link
                to="/register"
                class="mobile-nav-link"
                @click="mobileOpen = false"
              >
                <UserPlus class="h-4 w-4" />
                Register
              </router-link>
              <router-link
                to="/login"
                class="mobile-nav-link"
                @click="mobileOpen = false"
              >
                <LogIn class="h-4 w-4" />
                Login
              </router-link>
            </template>
            <a
              v-else
              href="#"
              class="mobile-nav-link"
              @click.prevent="logout(); mobileOpen = false"
            >
              <LogOut class="h-4 w-4" />
              Logout
            </a>
          </nav>
        </SheetContent>
      </Sheet>

      <!-- Brand -->
      <router-link
        to="/"
        class="mr-6 text-xl font-bold tracking-tight text-foreground no-underline hover:text-foreground"
      >
        {{ title }}
      </router-link>

      <!-- Desktop navigation -->
      <nav class="hidden md:flex items-center gap-1">
        <router-link
          to="/"
          class="desktop-nav-link"
          :class="{ active: route.path === '/' }"
        >
          Home
        </router-link>
        <router-link
          to="/lastheard"
          class="desktop-nav-link"
          :class="{ active: route.path === '/lastheard' }"
        >
          Last Heard
        </router-link>
        <router-link
          v-if="userStore.loggedIn"
          to="/repeaters"
          class="desktop-nav-link"
          :class="{ active: route.path === '/repeaters' }"
        >
          Repeaters
        </router-link>

        <!-- Talkgroups dropdown -->
        <div
          v-if="userStore.loggedIn"
          class="relative"
          @mouseenter="setMenuOpen('talkgroups', true)"
          @mouseleave="scheduleCloseMenus"
        >
          <DropdownMenuRoot
            :open="openTalkgroupsMenu"
            :modal="false"
            @update:open="setMenuOpen('talkgroups', $event)"
          >
            <DropdownMenuTrigger as-child>
              <button
                class="desktop-nav-link inline-flex items-center gap-1"
                :class="{ active: route.path.startsWith('/talkgroups') }"
                type="button"
              >
                Talkgroups
                <ChevronDown class="h-3 w-3" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              class="min-w-[10rem] rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
              :side-offset="4"
              align="start"
            >
              <DropdownMenuItem as-child>
                <router-link
                  to="/talkgroups"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  List
                </router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link
                  to="/talkgroups/owned"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  Owned
                </router-link>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuRoot>
        </div>

        <!-- OpenBridge Peers -->
        <router-link
          v-if="openBridgeFeature"
          to="/peers"
          class="desktop-nav-link"
          :class="{ active: route.path === '/peers' }"
        >
          OpenBridge Peers
        </router-link>

        <!-- Admin dropdown -->
        <div
          v-if="userStore.loggedIn && userStore.admin"
          class="relative"
          @mouseenter="setMenuOpen('admin', true)"
          @mouseleave="scheduleCloseMenus"
        >
          <DropdownMenuRoot
            :open="openAdminMenu"
            :modal="false"
            @update:open="setMenuOpen('admin', $event)"
          >
            <DropdownMenuTrigger as-child>
              <button
                class="desktop-nav-link inline-flex items-center gap-1"
                :class="{ active: route.path.startsWith('/admin') }"
                type="button"
              >
                Admin
                <ChevronDown class="h-3 w-3" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              class="min-w-[10rem] rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
              :side-offset="4"
              align="start"
            >
              <DropdownMenuItem as-child>
                <router-link
                  to="/admin/talkgroups"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  Talkgroups
                </router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link
                  to="/admin/repeaters"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  Repeaters
                </router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link
                  to="/admin/users"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  Users
                </router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link
                  to="/admin/users/approval"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  User Approvals
                </router-link>
              </DropdownMenuItem>
              <DropdownMenuItem as-child>
                <router-link
                  to="/admin/setup"
                  class="dropdown-link"
                  @click="closeMenus"
                >
                  Setup
                </router-link>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuRoot>
        </div>
      </nav>

      <!-- Right side: auth -->
      <div class="ml-auto hidden md:flex items-center gap-2">
        <template v-if="!userStore.loggedIn">
          <router-link to="/register" class="desktop-nav-link">
            Register
          </router-link>
          <router-link to="/login" class="desktop-nav-link">
            Login
          </router-link>
        </template>
        <a
          v-else
          href="#"
          class="desktop-nav-link"
          @click.prevent="logout()"
        >
          Logout
        </a>
      </div>
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
import {
  Antenna,
  ChevronDown,
  Globe,
  Home,
  LogIn,
  LogOut,
  Menu,
  MessageSquare,
  Radio,
  Settings,
  UserCheck,
  UserPlus,
  Users,
} from 'lucide-vue-next';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import API from '@/services/API';
import { useUserStore } from '@/store';

const userStore = useUserStore();
const route = useRoute();
const router = useRouter();

const title = ref(localStorage.getItem('title') || 'DMRHub');
const openBridgeFeature = ref(false);
const openTalkgroupsMenu = ref(false);
const openAdminMenu = ref(false);
const mobileOpen = ref(false);
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
/* Desktop nav links */
.desktop-nav-link {
  display: inline-flex;
  align-items: center;
  padding: 0.375rem 0.75rem;
  border-radius: 0.375rem;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--muted-foreground);
  text-decoration: none;
  transition: color 0.15s ease, background-color 0.15s ease;
}

.desktop-nav-link:hover {
  color: var(--foreground);
  background-color: var(--accent);
  text-decoration: none;
}

.desktop-nav-link.active,
.desktop-nav-link.router-link-exact-active {
  color: var(--foreground);
  background-color: var(--accent);
}

/* Dropdown links */
.dropdown-link {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.375rem 0.5rem;
  border-radius: 0.25rem;
  font-size: 0.875rem;
  color: var(--popover-foreground);
  text-decoration: none;
  cursor: pointer;
}

.dropdown-link:hover {
  background-color: var(--accent);
  text-decoration: none;
}

/* Mobile nav links */
.mobile-nav-link {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  border-radius: 0.375rem;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--foreground);
  text-decoration: none;
  transition: background-color 0.15s ease;
}

.mobile-nav-link:hover {
  background-color: var(--accent);
  text-decoration: none;
}

.mobile-nav-link.active,
.mobile-nav-link.router-link-exact-active {
  background-color: var(--accent);
  font-weight: 600;
}
</style>
