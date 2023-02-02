<template>
  <DataTable
    v-model:expandedRows="expandedRows"
    :value="talkgroups"
    :lazy="true"
    :paginator="true"
    :rows="10"
    :totalRecords="totalRecords"
    :loading="loading"
    @page="onPage($event)"
  >
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
        <span v-if="!slotProps.data.editable">
          <span v-if="slotProps.data.admins.length == 0">None</span>
          <span
            v-else
            v-bind:key="admin.id"
            v-for="admin in slotProps.data.admins"
          >
            {{ admin.display }}&nbsp;
          </span>
        </span>
        <span class="p-float-label" v-else>
          <MultiSelect
            id="admins"
            v-model="slotProps.data.admins"
            :options="allUsers"
            :filter="true"
            optionLabel="display"
            display="chip"
          >
            <template #chip="slotProps">
              {{ slotProps.value.display }}
            </template>
            <template #option="slotProps">
              {{ slotProps.option.display }}
            </template>
          </MultiSelect>
          <label for="admins">Admins</label>
        </span>
      </template>
    </Column>
    <Column field="ncos" header="Net Control Operators">
      <template #body="slotProps">
        <span v-if="!slotProps.data.editable">
          <span v-if="!slotProps.data.ncos || slotProps.data.ncos.length == 0"
            >None</span
          >
          <span v-else v-bind:key="nco.id" v-for="nco in slotProps.data.ncos">
            {{ nco.display }}&nbsp;
          </span>
        </span>
        <span class="p-float-label" v-else>
          <MultiSelect
            id="ncos"
            v-model="slotProps.data.ncos"
            :options="allUsers"
            :filter="true"
            optionLabel="display"
            display="chip"
          >
            <template #chip="slotProps">
              {{ slotProps.value.display }}
            </template>
            <template #option="slotProps">
              {{ slotProps.option.display }}
            </template>
          </MultiSelect>
          <label for="ncos">Net Control Operators</label>
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
import Chip from "primevue/chip/sfc";
import MultiSelect from "primevue/multiselect/sfc";

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
    MultiSelect,
    Chip,
  },
  data: function () {
    return {
      talkgroups: [],
      refresh: null,
      expandedRows: [],
      editableTalkgroups: 0,
      totalRecords: 0,
      loading: false,
      allUsers: [],
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
    if (this.socket) {
      this.socket.close();
    }
  },
  methods: {
    onPage(event) {
      this.loading = true;
      this.fetchData(event.page + 1, event.rows);
    },
    fetchData(page = 1, limit = 10) {
      if (this.editableTalkgroups > 0) {
        return;
      }
      if (this.$props.owner || this.$props.admin) {
        API.get("/users?limit=none")
          .then((res) => {
            var parrotIndex = -1;
            for (let i = 0; i < res.data.users.length; i++) {
              res.data.users[
                i
              ].display = `${res.data.users[i].id} - ${res.data.users[i].callsign}`;
              // Remove user with id 9990 (parrot)
              if (res.data.users[i].id === 9990) {
                parrotIndex = i;
              }
            }
            if (parrotIndex !== -1) {
              res.data.users.splice(parrotIndex, 1);
            }
            this.allUsers = res.data.users;
          })
          .catch((err) => {
            console.error(err);
          });
      }
      if (this.$props.owner) {
        API.get(`/talkgroups/my?limit=${limit}&page=${page}`)
          .then((res) => {
            this.talkgroups = this.cleanData(res.data.talkgroups);
            this.totalRecords = res.data.total;
            this.loading = false;
          })
          .catch((err) => {
            console.error(err);
          });
      } else {
        API.get(`/talkgroups?limit=${limit}&page=${page}`)
          .then((res) => {
            this.talkgroups = this.cleanData(res.data.talkgroups);
            this.totalRecords = res.data.total;
            this.loading = false;
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

        if (copyData[i].admins) {
          for (let j = 0; j < copyData[i].admins.length; j++) {
            copyData[i].admins[
              j
            ].display = `${copyData[i].admins[j].id} - ${copyData[i].admins[j].callsign}`;
          }
        }

        if (copyData[i].ncos) {
          for (let j = 0; j < copyData[i].ncos.length; j++) {
            copyData[i].ncos[
              j
            ].display = `${copyData[i].ncos[j].id} - ${copyData[i].ncos[j].callsign}`;
          }
        }
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
          API.post(`/talkgroups/${talkgroup.id}/admins`, {
            user_ids: talkgroup.admins.map((admin) => admin.id),
          })
            .then((res) => {
              API.post(`/talkgroups/${talkgroup.id}/ncos`, {
                user_ids: talkgroup.ncos.map((nco) => nco.id),
              })
                .then((res) => {
                  talkgroup.editable = false;
                  this.editableTalkgroups--;
                  if (this.editableTalkgroups == 0) {
                    this.fetchData();
                  }
                })
                .catch((err) => {
                  console.error(err);
                  if (
                    err.response &&
                    err.response.data &&
                    err.response.data.error
                  ) {
                    this.$toast.add({
                      severity: "error",
                      summary: "Error",
                      detail:
                        "Failed to update talkgroup admins: " +
                        err.response.data.error,
                      life: 3000,
                    });
                    return;
                  } else {
                    this.$toast.add({
                      severity: "error",
                      summary: "Error",
                      detail: "Failed to update talkgroup admins",
                      life: 3000,
                    });
                  }
                });
            })
            .catch((err) => {
              console.error(err);
              if (
                err.response &&
                err.response.data &&
                err.response.data.error
              ) {
                this.$toast.add({
                  severity: "error",
                  summary: "Error",
                  detail:
                    "Failed to update talkgroup admins: " +
                    err.response.data.error,
                  life: 3000,
                });
                return;
              } else {
                this.$toast.add({
                  severity: "error",
                  summary: "Error",
                  detail: "Failed to update talkgroup admins",
                  life: 3000,
                });
              }
            });
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
