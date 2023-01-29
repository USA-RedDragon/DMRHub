<template>
  <DataTable v-model:expandedRows="expandedRows" :value="talkgroups">
    <template v-if="this.$props.admin" #header>
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
    <Column :expander="true" v-if="this.$props.admin || this.$props.owner" />
    <Column field="id" header="Channel"></Column>
    <Column field="name" header="Name">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">{{ slotProps.data.name }}</span>
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
    <Column v-if="!this.$props.owner" field="admins" header="Admins">
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
    <Column field="ncos" header="Net Control Operators">
      <template #body="slotProps">
        <span v-if="!slotProps.data.ncos || slotProps.data.ncos.length == 0"
          >None</span
        >
        <span
          v-else
          v-bind:key="nco.callsign"
          v-for="nco in slotProps.data.ncos"
        >
          {{ nco.callsign }}&nbsp;
        </span>
      </template>
    </Column>
    <Column field="created_at" header="Created">
      <template #body="slotProps">{{
        slotProps.data.created_at.fromNow()
      }}</template>
    </Column>
    <template
      v-if="this.$props.admin || this.$props.owner"
      #expansion="slotProps"
    >
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
      <Button
        v-if="this.$props.admin && !slotProps.data.editable"
        class="p-button-raised p-button-rounded p-button-danger"
        icon="pi pi-trash"
        label="Delete"
        style="margin-left: 0.5em"
        @click="deleteTalkgroup(slotProps.data)"
      ></Button>
    </template>
  </DataTable>
</template>

<script>
import Button from "primevue/button/sfc";
import DataTable from "primevue/datatable/sfc";
import Column from "primevue/column/sfc";
import InputText from "primevue/inputtext/sfc";

import moment from "moment";

import { mapStores } from "pinia";
import { useSettingsStore } from "@/store";

import API from "@/services/API";

export default {
  name: "TalkgroupTable",
  props: {
    admin: Boolean,
    owner: Boolean,
  },
  components: {
    Button,
    DataTable,
    Column,
    InputText,
  },
  data: function () {
    return {
      talkgroups: [],
      refresh: null,
      expandedRows: [],
      editableTalkgroups: 0,
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
      if (this.editableTalkgroups > 0) {
        return;
      }
      if (this.$props.owner) {
        API.get("/talkgroups/my")
          .then((res) => {
            this.talkgroups = this.cleanData(res.data);
          })
          .catch((err) => {
            console.error(err);
          });
      } else {
        API.get("/talkgroups")
          .then((res) => {
            this.talkgroups = this.cleanData(res.data);
          })
          .catch((err) => {
            console.error(err);
          });
      }
    },
    cleanData(data) {
      let copyData = JSON.parse(JSON.stringify(data));

      for (let i = 0; i < copyData.length; i++) {
        copyData[i].created_at = moment(copyData[i].created_at);
        copyData[i].editable = false;
      }
      return copyData;
    },
    startEdit(talkgroup) {
      this.editableTalkgroups++;
      talkgroup.editable = true;
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
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: "Failed to update talkgroup: " + err.response.data.error,
              life: 3000,
            });
            return;
          } else {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: "Failed to update talkgroup",
              life: 3000,
            });
          }
        });
    },
  },
  computed: {
    ...mapStores(useSettingsStore),
  },
};
</script>

<style scoped>
.table-header-container {
  display: flex;
  justify-content: flex-end;
}
</style>
