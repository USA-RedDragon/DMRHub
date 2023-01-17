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
          <Column field="name" header="Name">
            <template #body="slotProps">
              <span v-if="!slotProps.data.editable">{{
                slotProps.data.name
              }}</span>
              <InputText
                v-if="slotProps.data.editable"
                v-model="slotProps.data.name"
              />
            </template>
          </Column>
          <Column field="description" header="Description">
            <template #body="slotProps">
              <span v-if="!slotProps.data.editable">{{
                slotProps.data.description
              }}</span>
              <InputText
                v-if="slotProps.data.editable"
                v-model="slotProps.data.description"
              />
            </template>
          </Column>
          <Column field="created_at" header="Created">
            <template #body="slotProps">{{
              slotProps.data.created_at.fromNow()
            }}</template>
          </Column>
          <template #expansion="slotProps">
            <Button
              v-if="!slotProps.data.editable"
              class="p-button-raised p-button-rounded p-button-primary"
              icon="pi pi-pencil"
              label="Edit"
              @click="startEdit(slotProps.data)"
            ></Button>
            <Button
              v-if="slotProps.data.editable"
              class="p-button-raised p-button-rounded p-button-success"
              icon="pi pi-check"
              label="Save"
              @click="saveTalkgroup(slotProps.data)"
            ></Button>
            <Button
              v-if="slotProps.data.editable"
              class="p-button-raised p-button-rounded p-button-primary"
              icon="pi pi-ban"
              label="Cancel"
              style="margin-left: 0.5em"
              @click="cancelEdit(slotProps.data)"
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
import InputText from "primevue/inputtext/sfc";

import { mapStores } from "pinia";
import { useSettingsStore } from "@/store";

export default {
  components: {
    Button,
    Card,
    Checkbox,
    DataTable,
    Column,
    InputText,
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
      editableTalkgroups: 0,
    };
  },
  methods: {
    fetchData() {
      if (!this.editableTalkgroups > 0) {
        API.get("/talkgroups/my")
          .then((res) => {
            this.talkgroups = res.data;
            for (let i = 0; i < this.talkgroups.length; i++) {
              this.talkgroups[i].created_at = moment(
                this.talkgroups[i].created_at
              );
              this.talkgroups[i].editable = false;
            }
          })
          .catch((err) => {
            console.error(err);
          });
      }
    },
    startEdit(talkgroup) {
      talkgroup.editable = true;
      this.editableTalkgroups++;
    },
    cancelEdit(talkgroup) {
      talkgroup.editable = false;
      this.editableTalkgroups--;
      if (this.editableTalkgroups == 0) {
        this.fetchData();
      }
    },
    saveTalkgroup(talkgroup) {
      API.patch("/talkgroups/" + talkgroup.id, {
        name: talkgroup.name,
        description: talkgroup.description,
      })
        .then((res) => {
          this.$toast.add({
            severity: "success",
            summary: "Success",
            detail: "Talkgroup updated",
            life: 3000,
          });
          talkgroup.editable = false;
          this.editableTalkgroups--;
          if (this.editableTalkgroups == 0) {
            this.fetchData();
          }
        })
        .catch((err) => {
          console.error(err);
          if (err.response.data.error) {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: "Failed to update talkgroup: " + err.response.data.error,
              life: 3000,
            });
            return;
          }
          this.$toast.add({
            severity: "error",
            summary: "Error",
            detail: "Failed to update talkgroup",
            life: 3000,
          });
        });
    },
  },
  computed: {
    ...mapStores(useSettingsStore),
  },
};
</script>

<style scoped></style>
