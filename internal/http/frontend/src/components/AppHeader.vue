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
  <header>
    <h1>
      <router-link to="/">{{ title }}</router-link>
    </h1>
    <div class="wrapper">
      <nav>
        <router-link to="/">Home</router-link>
        <router-link v-if="this.userStore.loggedIn" to="/repeaters">Repeaters</router-link>
        <router-link v-if="this.userStore.loggedIn" to="#" custom>
          <a
            href="#"
            @click="toggleTalkgroupsMenu"
            :class="{
              adminNavLink: true,
              'router-link-active': this.$route.path.startsWith('/talkgroups'),
            }"
            >Talkgroups</a
          >
        </router-link>
        <PVMenu
          v-if="this.userStore.loggedIn"
          ref="talkgroupsMenu"
          :popup="true"
          :model="[
            {
              label: '&nbsp;&nbsp;List',
              to: '/talkgroups',
            },
            {
              label: '&nbsp;&nbsp;Owned',
              to: '/talkgroups/owned',
            },
          ]"
        >
          <template #item="{ item }">
            <router-link
              :to="item.to"
              custom
              v-slot="{ href, navigate, isActive, isExactActive }"
            >
              <a
                :href="href"
                @click="navigate"
                :class="{
                  adminNavLink: true,
                  'router-link-active': isActive,
                  'router-link-active-exact': isExactActive,
                }"
              >
                <div>{{ item.label }}</div>
              </a>
            </router-link>
          </template>
        </PVMenu>

        <router-link v-if="this.openBridgeFeature" to="/peers">OpenBridge Peers</router-link>
        <router-link
          v-if="this.userStore.loggedIn && this.userStore.admin"
          to="#"
          custom
        >
          <a
            href="#"
            @click="toggleAdminMenu"
            :class="{
              adminNavLink: true,
              'router-link-active': this.$route.path.startsWith('/admin'),
            }"
            >Admin</a
          >
        </router-link>
        <PVMenu
          v-if="this.userStore.loggedIn && this.userStore.admin"
          ref="adminMenu"
          :popup="true"
          :model="this.adminMenu"
        >
          <template #item="{ item }">
            <router-link
              :to="item.to"
              custom
              v-slot="{ href, navigate, isActive, isExactActive }"
            >
              <a
                :href="href"
                @click="navigate"
                :class="{
                  adminNavLink: true,
                  'router-link-active': isActive,
                  'router-link-active-exact': isExactActive,
                }"
              >
                <div>{{ item.label }}</div>
              </a>
            </router-link>
          </template>
        </PVMenu>
        <router-link v-if="!this.userStore.loggedIn" to="/register"
          >Register</router-link
        >
        <router-link v-if="!this.userStore.loggedIn" to="/login"
          >Login</router-link
        >
        <a v-else href="#" @click="logout()">Logout</a>
      </nav>
    </div>
  </header>
</template>

<script>
import Menu from 'primevue/menu';
import API from '@/services/API';
import features from '@/services/features';

import { mapStores } from 'pinia';
import { useUserStore } from '@/store';

export default {
  components: {
    PVMenu: Menu,
  },
  data: function() {
    return {
      title: localStorage.getItem('title') || 'DMRHub',
      openBridgeFeature: false,
      adminMenu: [
        {
          label: '\xa0\xa0Talkgroups',
          to: '/admin/talkgroups',
        },
        {
          label: '\xa0\xa0Repeaters',
          to: '/admin/repeaters',
        },
        {
          label: '\xa0\xa0Users',
          to: '/admin/users',
        },
        {
          label: '\xa0\xa0User Approvals',
          to: '/admin/users/approval',
        },
      ],
    };
  },
  created() {
    features.getFeatures().then(() => {
      if (features.isEnabled(features.OpenBridge)) {
        this.openBridgeFeature = true;
        this.adminMenu.concat(
          {
            label: '\xa0\xa0OpenBridge Peers',
            to: '/admin/peers',
          },
        );
      }
    });
    this.getTitle();
    this.userHasOpenBridgePeers();
  },
  mounted() {},
  methods: {
    setTitle(title) {
      localStorage.setItem('title', title);
      this.title = title;
    },
    getTitle() {
      API.get('/network/name')
        .then((response) => {
          this.setTitle(response.data);
        })
        .catch((error) => {
          console.log(error);
        });
    },
    userHasOpenBridgePeers() {
      if (!this.openBridgeFeature) {
        this.userStore.hasOpenBridgePeers = false;
        return;
      }
      API.get('/peers/my')
        .then((response) => {
          if ('total' in response.data && response.data.total > 0) {
            this.userStore.hasOpenBridgePeers = true;
          } else {
            this.userStore.hasOpenBridgePeers = false;
          }
        })
        .catch((error) => {
          console.log(error);
        });
    },
    logout() {
      API.get('/auth/logout')
        .then((_res) => {
          this.userStore.loggedIn = false;
          this.$router.push('/login');
        })
        .catch((err) => {
          console.error(err);
        });
    },
    toggleAdminMenu(event) {
      this.$refs.adminMenu.toggle(event);
    },
    toggleTalkgroupsMenu(event) {
      this.$refs.talkgroupsMenu.toggle(event);
    },
  },
  computed: {
    ...mapStores(useUserStore),
  },
};
</script>

<style scoped>
header {
  text-align: center;
  max-height: 100vh;
}

header a,
.adminNavLink {
  text-decoration: none;
}

header a,
header a:visited,
header a:link,
.adminNavLink:visited,
.adminNavLink:link {
  color: var(--primary-text-color);
}

header nav .router-link-active,
.adminNavLink.router-link-active {
  color: var(--cyan-300) !important;
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
}

nav a {
  display: inline-block;
  padding: 0 1rem;
  border-left: 2px solid #444;
}

nav a:first-of-type {
  border: 0;
}
</style>
