<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Users</template>
      <template #content>
        <DataTable
          :value="users"
          v-model:expandedRows="expandedRows"
          dataKey="id"
        >
          <Column :expander="true" />
          <Column field="id" header="DMR ID"></Column>
          <Column field="callsign" header="Callsign"></Column>
          <Column field="username" header="Username"></Column>
          <Column field="approved" header="Approve?">
            <template #body="slotProps">
              <Checkbox v-model="slotProps.data.approved" :binary="true" />
            </template>
          </Column>
          <Column field="admin" header="Admin?">
            <template #body="slotProps">
              <Checkbox v-model="slotProps.data.admin" :binary="true" />
            </template>
          </Column>
          <Column field="repeaters" header="Repeater Count"></Column>
          <Column field="created_at" header="Created"></Column>
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
    </Card>
  </div>
</template>

<script>
import Button from "primevue/button/sfc";
import Card from "primevue/card/sfc";
import Checkbox from "primevue/checkbox/sfc";
import DataTable from "primevue/datatable/sfc";
import Column from "primevue/column/sfc";
import ColumnGroup from "primevue/columngroup/sfc"; //optional for column grouping
import Row from "primevue/row/sfc";
import API from "@/services/API";
import moment from "moment";

export default {
  components: {
    Button,
    Card,
    Checkbox,
    DataTable,
    Column,
    ColumnGroup,
    Row,
  },
  created() {},
  mounted() {
    this.fetchData();
  },
  data: function () {
    return {
      users: [],
      expandedRows: [],
    };
  },
  methods: {
    fetchData() {
      API.get("/users")
        .then((res) => {
          this.users = res.data;
          for (let i = 0; i < this.users.length; i++) {
            this.users[i].repeaters = this.users[i].repeaters.length;

            this.users[i].created_at = moment(
              this.users[i].created_at
            ).fromNow();
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
    handleApprove(event, user) {
      this.$toast.add({
        summary: "Not Implemented",
        severity: "error",
        detail: `Users cannot be edited yet.`,
        life: 3000,
      });
    },
    handleAdmin(event, user) {
      this.$toast.add({
        summary: "Not Implemented",
        severity: "error",
        detail: `Users cannot be edited yet.`,
        life: 3000,
      });
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
    },
  },
};
</script>

<style scoped></style>
