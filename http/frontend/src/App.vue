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
import { useUserStore, useSettingsStore } from "@/store";

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
  created() {
    this.socket = new WebSocket(this.getWebsocketURI() + "/health");
    this.mapSocketEvents();
  },
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
    getWebsocketURI() {
      var loc = window.location;
      var new_uri;
      if (loc.protocol === "https:") {
        new_uri = "wss:";
      } else {
        new_uri = "ws:";
      }
      // nodejs development
      if (window.location.port == 5173) {
        // Change port to 3005
        new_uri += "//" + loc.hostname + ":3005";
      } else {
        new_uri += "//" + loc.host;
      }
      new_uri += "/ws";
      console.log('Websocket URI: "' + new_uri + '"');
      return new_uri;
    },
    mapSocketEvents() {
      this.socket.addEventListener("open", (event) => {
        console.log("Connected to websocket");
        setInterval(() => {
          this.socket.send("PING");
        }, 500);
      });

      this.socket.addEventListener("close", (event) => {
        console.error("Disconnected from websocket");
        console.error("Sleeping for 1 second before reconnecting");
        setTimeout(() => {
          this.socket = new WebSocket(this.getWebsocketURI() + "/health");
          this.mapSocketEvents();
        }, 1000);
      });

      this.socket.addEventListener("error", (event) => {
        console.error("Error from websocket", event);
        this.socket = new WebSocket(this.getWebsocketURI() + "/health");
        this.mapSocketEvents();
      });

      this.socket.addEventListener("message", (event) => {
        if (event.data === "PONG") {
          return;
        }
        console.log("Message from websocket", event.data);
      });
    },
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
