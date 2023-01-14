<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Repeaters</template>
      <template #content>
        <DataTable
          :value="repeaters"
          v-model:expandedRows="expandedRows"
          dataKey="id"
        >
          <Column :expander="true" />
          <Column field="id" header="DMR Radio ID"></Column>
          <Column field="connected_time" header="Connected Time"></Column>
          <Column field="last_ping_time" header="Last Ping"></Column>
          <Column
            field="ts1_static_talkgroups"
            header="TS1 Static TGs"
          ></Column>
          <Column
            field="ts2_static_talkgroups"
            header="TS2 Static TGs"
          ></Column>
          <Column
            field="ts1_dynamic_talkgroup"
            header="TS1 Dynamic TG"
          ></Column>
          <Column
            field="ts2_dynamic_talkgroup"
            header="TS2 Dynamic TG"
          ></Column>
          <Column field="created_at" header="Created At"></Column>
          <template #expansion="slotProps">
            <Button
              class="p-button-raised p-button-rounded p-button-primary"
              icon="pi pi-pencil"
              label="Edit"
              @click="editRepeater(slotProps.data)"
            ></Button>
            <Button
              class="p-button-raised p-button-rounded p-button-danger"
              icon="pi pi-trash"
              label="Delete"
              style="margin-left: 0.5em"
              @click="deleteRepeater(slotProps.data)"
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
      repeaters: [],
      expandedRows: [],
    };
  },
  methods: {
    fetchData() {
      API.get("/repeaters")
        .then((res) => {
          this.repeaters = res.data;
          for (let i = 0; i < this.repeaters.length; i++) {
            if (this.repeaters[i].ts1_dynamic_talkgroup.id == 0) {
              this.repeaters[i].ts1_dynamic_talkgroup = "Not Linked";
            }
            // If repeater ts2_dynamic_talkgroup.id is 0, then set ts2_dynamic_talkgroup to null
            if (this.repeaters[i].ts2_dynamic_talkgroup.id == 0) {
              this.repeaters[i].ts2_dynamic_talkgroup = "Not Linked";
            }

            if (this.repeaters[i].ts1_static_talkgroups == null) {
              this.repeaters[i].ts1_static_talkgroups = "None";
            }

            if (this.repeaters[i].ts2_static_talkgroups == null) {
              this.repeaters[i].ts2_static_talkgroups = "None";
            }
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
    deleteRepeater(repeater) {
      // First, show a confirmation dialog
      this.$confirm.require({
        message: "Are you sure you want to delete this repeater?",
        header: "Delete Repeater",
        icon: "pi pi-exclamation-triangle",
        acceptClass: "p-button-danger",
        accept: () => {
          API.delete("/repeaters/" + repeater.id)
            .then((res) => {
              this.$toast.add({
                summary: "Confirmed",
                severity: "success",
                detail: `Repeater ${repeater.id} deleted`,
                life: 3000,
              });
              this.fetchData();
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                severity: "danger",
                summary: "Error",
                detail: `Error deleting repeater ${repeater.id}`,
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

<style scoped>
.table-header-container {
  display: flex;
  justify-content: flex-end;
}
</style>
