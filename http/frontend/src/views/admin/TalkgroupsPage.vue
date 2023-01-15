<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Talkgroups</template>
      <template #content>
        <DataTable
          :value="talkgroups"
          v-model:expandedRows="expandedRows"
          dataKey="id"
        >
          <template #header>
            <div class="table-header-container">
              <RouterLink to="/admin/talkgroups/new">
                <Button
                  class="p-button-raised p-button-rounded p-button-success"
                  icon="pi pi-plus"
                  label="Add New Talkgroup"
                />
              </RouterLink>
            </div>
          </template>
          <Column :expander="true" />
          <Column field="id" header="Channel"></Column>
          <Column field="name" header="Name"></Column>
          <Column field="description" header="Description"></Column>
          <Column field="admins" header="Admins">
            <template #body="slotProps">
              <span v-if="slotProps.data.admins.length == 0">None</span>
              <span
                v-else
                v-bind:key="admin.callsign"
                v-for="admin in slotProps.data.admins"
              >
                {{ admin.callsign }}&nbsp;
              </span>
            </template>
          </Column>
          <Column field="created_at" header="Created"></Column>
          <template #expansion="slotProps">
            <Button
              class="p-button-raised p-button-rounded p-button-primary"
              icon="pi pi-pencil"
              label="Edit"
              @click="editTalkgroup(slotProps.data)"
            ></Button>
            <Button
              class="p-button-raised p-button-rounded p-button-danger"
              icon="pi pi-trash"
              label="Delete"
              style="margin-left: 0.5em"
              @click="deleteTalkgroup(slotProps.data)"
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
import moment from "moment";
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
    setInterval(this.fetchData, 3000);
  },
  data: function () {
    return {
      talkgroups: [],
      expandedRows: [],
    };
  },
  methods: {
    fetchData() {
      API.get("/talkgroups")
        .then((res) => {
          this.talkgroups = res.data;
          for (let i = 0; i < this.talkgroups.length; i++) {
            this.talkgroups[i].created_at = moment(
              this.talkgroups[i].created_at
            ).fromNow();
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
    deleteTalkgroup(talkgroup) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: "Are you sure you want to delete this talkgroup?",
        header: "Delete Talkgroup",
        icon: "pi pi-exclamation-triangle",
        acceptClass: "p-button-danger",
        accept: () => {
          API.delete("/talkgroups/" + talkgroup.id)
            .then((res) => {
              this.$toast.add({
                summary: "Confirmed",
                severity: "success",
                detail: `Talkgroup ${talkgroup.id} deleted`,
                life: 3000,
              });
              this.fetchData();
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                severity: "error",
                summary: "Error",
                detail: `Error deleting talkgroup ${talkgroup.id}`,
                life: 3000,
              });
            });
        },
        reject: () => {},
      });
    },
    editTalkgroup(talkgroup) {
      this.$toast.add({
        summary: "Not Implemented",
        severity: "error",
        detail: `Talkgroups cannot be edited yet.`,
        life: 3000,
      });
    },
  },
};
</script>

<style scoped>
.table-header-container {
  display: flex;
  justify-content: flex-end;
}
</style>
