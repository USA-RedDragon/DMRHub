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
    editTalkgroup(talkgroup) {},
  },
};
</script>

<style scoped></style>
