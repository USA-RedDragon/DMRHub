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
          <Column field="created_at" header="Created"></Column>
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
import moment from "moment";
import API from "@/services/API";

import { mapStores } from "pinia";
import { useSettingsStore } from "@/store";

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
    this.refresh = setInterval(
      this.fetchData,
      this.settingsStore.refreshInterval
    );
  },
  unmounted() {
    clearInterval(this.refresh);
  },
  data: function () {
    return {
      talkgroups: [],
      expandedRows: [],
      refresh: null,
    };
  },
  methods: {
    fetchData() {
      API.get("/talkgroups/my")
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
    editTalkgroup(talkgroup) {
      this.$toast.add({
        summary: "Not Implemented",
        severity: "error",
        detail: `Talkgroups cannot be edited yet.`,
        life: 3000,
      });
    },
  },
  computed: {
    ...mapStores(useSettingsStore),
  },
};
</script>

<style scoped></style>
