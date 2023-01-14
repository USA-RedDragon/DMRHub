<template>
  <Header />
  <RouterView />
  <Footer />
</template>

<script>
import { RouterView } from "vue-router";
import Footer from "./components/Footer.vue";
import Header from "./components/Header.vue";
import API from "@/services/API";

import { mapStores } from "pinia";
import { useUserStore } from "@/store";

export default {
  name: "App",
  components: {
    Header,
    Footer,
  },
  data() {
    return {
      // localStorage in Firefox is string-only
      dark: localStorage.dark === "true" ? true : false,
      user: {},
    };
  },
  watch: {
    dark(_newValue) {
      // localStorage in Firefox is string-only
      localStorage.dark = this.dark ? "true" : "false";
    },
  },
  mounted() {
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
  computed: {
    ...mapStores(useUserStore),
  },
};
</script>

<style scoped></style>
