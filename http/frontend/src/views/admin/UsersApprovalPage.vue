<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>User Approvals</template>
      <template #content>
        <DataTable :value="users">
          <Column field="id" header="DMR ID"></Column>
          <Column field="callsign" header="Callsign"></Column>
          <Column field="username" header="Username"></Column>
          <Column field="approved" header="Approve?">
            <template #body="slotProps">
              <Button
                label="Approve"
                class="p-button-raised p-button-rounded"
                @click="handleApprove(slotProps.data)"
              />
            </template>
          </Column>
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
    };
  },
  methods: {
    fetchData() {
      API.get("/users")
        .then((res) => {
          this.users = res.data.filter(function (itm) {
            return itm.approved == false;
          });
        })
        .catch((err) => {
          console.error(err);
        });
    },
    handleApprove(user) {
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
  },
};
</script>

<style scoped></style>
