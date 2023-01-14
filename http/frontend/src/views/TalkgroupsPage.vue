<template>
  <div>
    <Toast />
    <ConfirmDialog></ConfirmDialog>
    <Card>
      <template #title>Talkgroups</template>
      <template #content>
        <DataTable :value="talkgroups">
          <Column field="id" header="Channel"></Column>
          <Column field="name" header="Name"></Column>
          <Column field="description" header="Description"></Column>
          <Column field="admins" header="Admins"></Column>
          <Column field="created_at" header="Created At"></Column>
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
      API.get("/talkgroups")
        .then((res) => {
          this.talkgroups = res.data;
        })
        .catch((err) => {
          console.error(err);
        });
    },
  },
};
</script>

<style scoped></style>
