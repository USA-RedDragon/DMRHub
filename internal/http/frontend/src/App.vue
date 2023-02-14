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
  <Header />
  <RouterView />
  <Footer />
  <ThemeConfig />
</template>

<script>
import { RouterView } from "vue-router";
import Footer from "./components/Footer.vue";
import Header from "./components/Header.vue";
import ThemeConfig from "./components/ThemeConfig.vue";
import API from "@/services/API";

import { mapStores } from "pinia";
import { useUserStore, useSettingsStore } from "@/store";

export default {
  name: "App",
  components: {
    RouterView,
    Header,
    Footer,
    ThemeConfig,
  },
  data() {
    return {
      // localStorage in Firefox is string-only
      dark: localStorage.dark === "true" ? true : false,
      refresh: null,
      socket: null,
    };
  },
  watch: {
    dark(_newValue) {
      // localStorage in Firefox is string-only
      localStorage.dark = this.dark ? "true" : "false";
    },
  },
  created() {},
  mounted() {
    this.fetchData();
    this.refresh = setInterval(
      this.fetchData,
      this.settingsStore.refreshInterval
    );
  },
  unmounted() {
    clearInterval(this.refresh);
  },
  methods: {
    fetchData() {
      // GET /users/me
      API.get("/users/me")
        .then((res) => {
          this.userStore.id = res.data.id;
          this.userStore.callsign = res.data.callsign;
          this.userStore.username = res.data.username;
          this.userStore.admin = res.data.admin;
          this.userStore.created_at = res.data.created_at;
          this.userStore.loggedIn = true;
        })
        .catch((err) => {
          this.userStore.loggedIn = false;
        });
    },
  },
  computed: {
    ...mapStores(useUserStore, useSettingsStore),
  },
};
</script>

<style scoped></style>
