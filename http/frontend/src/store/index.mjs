import { defineStore } from "pinia";

export const useUserStore = defineStore("user", {
  state: () => ({
    loggedIn: false,
    id: 0,
    callsign: "",
    username: "",
    admin: false,
    created_at: "",
  }),
  getters: {},
  actions: {},
});

export const useSettingsStore = defineStore("settings", {
  state: () => ({
    refreshInterval: 5000,
  }),
  getters: {},
  actions: {},
});
