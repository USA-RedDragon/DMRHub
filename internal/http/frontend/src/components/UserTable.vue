<template>
  <DataTable :value="users" v-model:expandedRows="expandedRows" dataKey="id">
    <Column :expander="true" v-if="!this.$props.approval" />
    <Column field="id" header="DMR ID"></Column>
    <Column field="callsign" header="Callsign"></Column>
    <Column field="username" header="Username"></Column>
    <Column field="approved" header="Approve?">
      <template #body="slotProps" v-if="this.$props.approval">
        <Button
          label="Approve"
          class="p-button-raised p-button-rounded"
          @click="handleApprovePage(slotProps.data)"
        />
      </template>
      <template #body="slotProps" v-else>
        <Checkbox
          v-model="slotProps.data.approved"
          :binary="true"
          @change="handleApprove($event, slotProps.data)"
        />
      </template>
    </Column>
    <Column field="admin" header="Admin?" v-if="!this.$props.approval">
      <template #body="slotProps">
        <Checkbox
          v-model="slotProps.data.admin"
          :binary="true"
          @change="handleAdmin($event, slotProps.data)"
        />
      </template>
    </Column>
    <Column
      field="repeaters"
      header="Repeater Count"
      v-if="!this.$props.approval"
    ></Column>
    <Column field="created_at" header="Created">
      <template #body="slotProps">{{
        slotProps.data.created_at.fromNow()
      }}</template>
    </Column>
    <template #expansion="slotProps">
      <Button
        class="p-button-raised p-button-rounded p-button-primary"
        icon="pi pi-pencil"
        label="Edit"
        @click="editUser(slotProps.data)"
      ></Button>
      <Button
        class="p-button-raised p-button-rounded p-button-danger"
        icon="pi pi-trash"
        label="Delete"
        style="margin-left: 0.5em"
        @click="deleteUser(slotProps.data)"
      ></Button>
    </template>
  </DataTable>
</template>

<script>
import Button from "primevue/button/sfc";
import Checkbox from "primevue/checkbox/sfc";
import DataTable from "primevue/datatable/sfc";
import Column from "primevue/column/sfc";

import moment from "moment";

import { mapStores } from "pinia";
import { useUserStore, useSettingsStore } from "@/store";

import API from "@/services/API";

export default {
  name: "UserTable",
  props: {
    approval: Boolean,
  },
  components: {
    Button,
    Checkbox,
    DataTable,
    Column,
  },
  data: function () {
    return {
      users: [],
      expandedRows: [],
      refresh: null,
    };
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
    fetchData() {
      API.get("/users")
        .then((res) => {
          if (this.$props.approval) {
            res.data = res.data.filter(function (itm) {
              return itm.approved == false;
            });
          }
          for (let i = 0; i < res.data.length; i++) {
            res.data[i].repeaters = res.data[i].repeaters.length;

            res.data[i].created_at = moment(res.data[i].created_at);
          }
          this.users = res.data;
        })
        .catch((err) => {
          console.error(err);
        });
    },
    handleApprovePage(user) {
      this.$confirm.require({
        message: "Are you sure you want to approve this user?",
        header: "Approve User",
        icon: "pi pi-exclamation-triangle",
        acceptClass: "p-button-danger",
        accept: () => {
          API.post("/users/approve/" + user.id, {})
            .then((res) => {
              // Refresh user data
              this.fetchData();
              this.$toast.add({
                summary: "Confirmed",
                severity: "success",
                detail: `User ${user.id} approved`,
                life: 3000,
              });
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                summary: "Error",
                severity: "error",
                detail: `Error approving user ${user.id}`,
                life: 3000,
              });
            });
        },
        reject: () => {},
      });
    },
    handleApprove(event, user) {
      var action = user.approved ? "approve" : "suspend";
      var actionVerb = user.approved ? "approved" : "suspended";
      // Don't allow the user to uncheck the admin box
      API.post(`/users/${action}/${user.id}`, {})
        .then(() => {
          this.$toast.add({
            summary: "Confirmed",
            severity: "success",
            detail: `User ${user.id} ${actionVerb}`,
            life: 3000,
          });
          this.fetchData();
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: "An unknown error occurred",
              life: 3000,
            });
          }
          this.fetchData();
        });
    },
    handleAdmin(event, user) {
      var action = user.admin ? "promote" : "demote";
      var actionVerb = user.admin ? "promoted" : "demoted";
      // Don't allow the user to uncheck the admin box
      if (this.userStore.id == 999999) {
        API.post(`/users/${action}/${user.id}`, {})
          .then(() => {
            this.$toast.add({
              summary: "Confirmed",
              severity: "success",
              detail: `User ${user.id} ${actionVerb}`,
              life: 3000,
            });
            this.fetchData();
          })
          .catch((err) => {
            console.error(err);
            if (err.response && err.response.data && err.response.data.error) {
              this.$toast.add({
                severity: "error",
                summary: "Error",
                detail: err.response.data.error,
                life: 3000,
              });
            } else {
              this.$toast.add({
                severity: "error",
                summary: "Error",
                detail: "An unknown error occurred",
                life: 3000,
              });
            }
            this.fetchData();
          });
      } else {
        this.$toast.add({
          summary: "Only the System Admin can do this.",
          severity: "error",
          detail: `Standard Admins cannot promote other users.`,
          life: 3000,
        });
        this.fetchData();
      }
    },
    editUser(user) {
      this.$toast.add({
        summary: "Not Implemented",
        severity: "error",
        detail: `Users cannot be edited yet.`,
        life: 3000,
      });
    },
    deleteUser(user) {
      if (this.userStore.id == 999999) {
        this.$confirm.require({
          message: "Are you sure you want to delete this user?",
          header: "Delete User",
          icon: "pi pi-exclamation-triangle",
          acceptClass: "p-button-danger",
          accept: () => {
            API.delete("/users/" + user.id)
              .then((res) => {
                this.$toast.add({
                  summary: "Confirmed",
                  severity: "success",
                  detail: `User ${user.id} deleted`,
                  life: 3000,
                });
                this.fetchData();
              })
              .catch((err) => {
                console.error(err);
                this.$toast.add({
                  severity: "error",
                  summary: "Error",
                  detail: `Error deleting user ${user.id}`,
                  life: 3000,
                });
              });
          },
          reject: () => {},
        });
      } else {
        this.$toast.add({
          summary: "Only the System Admin can do this.",
          severity: "error",
          detail: `Standard admins cannot delete other users.`,
          life: 3000,
        });
      }
    },
  },
  computed: {
    ...mapStores(useUserStore, useSettingsStore),
  },
};
</script>

<style scoped></style>
