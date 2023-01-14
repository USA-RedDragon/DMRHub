<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Owned Talkgroups</template>
      <template #content>
        <DataTable
          :value="talkgroups"
          v-model:expandedRows="expandedRows"
          dataKey="id"
        >
          <Column :expander="true" />
          <Column field="id" header="Channel"></Column>
          <Column field="name" header="Name"></Column>
          <Column field="description" header="Description"></Column>
          <Column field="admins" header="Admins"></Column>
          <Column field="created_at" header="Created At"></Column>
          <template #expansion="slotProps">
            <Button
              class="p-button-raised p-button-rounded p-button-primary"
              icon="pi pi-pencil"
              label="Edit"
              @click="editTalkgroup(slotProps.data)"
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
      talkgroups: [],
      expandedRows: [],
    };
  },
  methods: {
    fetchData() {
      API.get("/talkgroups/my")
        .then((res) => {
          this.talkgroups = res.data;
        })
        .catch((err) => {
          console.error(err);
        });
    },
    editTalkgroup(talkgroup) {
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
                severity: "danger",
                summary: "Error",
                detail: `Error deleting talkgroup ${talkgroup.id}`,
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
