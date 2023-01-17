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
          <template #header>
            <div class="table-header-container">
              <RouterLink to="/repeaters/new">
                <Button
                  class="p-button-raised p-button-rounded p-button-success"
                  icon="pi pi-plus"
                  label="Enroll New Repeater"
                />
              </RouterLink>
            </div>
          </template>
          <Column :expander="true" />
          <Column field="id" header="DMR Radio ID"></Column>
          <Column field="connected_time" header="Last Connected">
            <template #body="slotProps">
              <span v-if="slotProps.data.connected_time.year() != 0">
                {{ slotProps.data.connected_time.fromNow() }}
              </span>
              <span v-else>Never</span>
            </template>
          </Column>
          <Column field="last_ping_time" header="Last Ping">
            <template #body="slotProps">
              <span v-if="slotProps.data.last_ping_time.year() != 0">
                {{ slotProps.data.last_ping_time.fromNow() }}
              </span>
              <span v-else>Never</span>
            </template>
          </Column>
          <Column field="ts1_static_talkgroups" header="TS1 Static TGs">
            <template #body="slotProps">
              <span v-if="!slotProps.data.editable">
                <span v-if="slotProps.data.ts1_static_talkgroups.length == 0"
                  >None</span
                >
                <span
                  v-else
                  v-bind:key="tg.id"
                  class="chips"
                  v-for="tg in slotProps.data.ts1_static_talkgroups"
                >
                  <Chip :label="tg.id + ' - ' + tg.name"></Chip>
                </span>
              </span>
              <span class="p-float-label" v-else>
                <MultiSelect
                  id="ts1_static_talkgroups"
                  v-model="slotProps.data.ts1_static_talkgroups"
                  :options="this.talkgroups"
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
                <label for="ts1_static_talkgroups">TGs</label>
              </span>
            </template>
          </Column>
          <Column field="ts2_static_talkgroups" header="TS2 Static TGs">
            <template #body="slotProps">
              <span v-if="!slotProps.data.editable">
                <span v-if="slotProps.data.ts2_static_talkgroups.length == 0"
                  >None</span
                >
                <span
                  v-else
                  v-bind:key="tg.id"
                  class="chips"
                  v-for="tg in slotProps.data.ts2_static_talkgroups"
                >
                  <Chip :label="tg.id + ' - ' + tg.name"></Chip>
                </span>
              </span>
              <span class="p-float-label" v-else>
                <MultiSelect
                  id="ts2_static_talkgroups"
                  v-model="slotProps.data.ts2_static_talkgroups"
                  :options="this.talkgroups"
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
                <label for="ts2_static_talkgroups">TGs</label>
              </span>
            </template>
          </Column>
          <Column field="ts1_dynamic_talkgroup" header="TS1 Dynamic TG">
            <template #body="slotProps">
              <span v-if="!slotProps.data.editable">
                <span v-if="slotProps.data.ts1_dynamic_talkgroup.id == 0"
                  >None</span
                >
                <Chip
                  v-else
                  :label="
                    slotProps.data.ts1_dynamic_talkgroup.id +
                    ' - ' +
                    slotProps.data.ts1_dynamic_talkgroup.name
                  "
                ></Chip>
              </span>
              <span class="p-float-label" v-else>
                <Dropdown
                  id="ts1_dynamic_talkgroup"
                  v-model="slotProps.data.ts1_dynamic_talkgroup"
                  :options="this.talkgroups"
                  :filter="true"
                  optionLabel="display"
                  display="chip"
                >
                  <template #value="slotProps">
                    <Chip
                      :label="slotProps.value.display"
                      v-if="slotProps.value.id != 0"
                    ></Chip>
                  </template>
                  <template #option="slotProps">
                    {{ slotProps.option.display }}
                  </template>
                </Dropdown>
                <label for="ts1_dynamic_talkgroup">TG</label>
              </span>
            </template>
          </Column>
          <Column field="ts2_dynamic_talkgroup" header="TS2 Dynamic TG">
            <template #body="slotProps">
              <span v-if="!slotProps.data.editable">
                <span v-if="slotProps.data.ts2_dynamic_talkgroup.id == 0">
                  None
                </span>
                <Chip
                  v-else
                  :label="
                    slotProps.data.ts2_dynamic_talkgroup.id +
                    ' - ' +
                    slotProps.data.ts2_dynamic_talkgroup.name
                  "
                ></Chip>
              </span>
              <span class="p-float-label" v-else>
                <Dropdown
                  id="ts2_dynamic_talkgroup"
                  v-model="slotProps.data.ts2_dynamic_talkgroup"
                  :options="this.talkgroups"
                  :filter="true"
                  optionLabel="display"
                  display="chip"
                >
                  <template #value="slotProps">
                    <Chip
                      :label="slotProps.value.display"
                      v-if="slotProps.value.id != 0"
                    ></Chip>
                  </template>
                  <template #option="slotProps">
                    {{ slotProps.option.display }}
                  </template>
                </Dropdown>
                <label for="ts2_dynamic_talkgroup">TG</label>
              </span>
            </template>
          </Column>
          <Column field="hotspot" header="Hotspot"></Column>
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
              label="Edit Talkgroups"
              @click="startEdit(slotProps.data)"
            ></Button>
            <Button
              v-if="slotProps.data.editable"
              class="p-button-raised p-button-rounded p-button-success"
              icon="pi pi-check"
              label="Save Talkgroups"
              @click="saveTalkgroups(slotProps.data)"
            ></Button>
            <Button
              class="p-button-raised p-button-rounded p-button-secondary"
              icon="pi pi-link"
              label="Unlink Dynamic TS1"
              style="margin-left: 0.5em"
              @click="unlink(1, slotProps.data)"
            ></Button>
            <Button
              class="p-button-raised p-button-rounded p-button-secondary"
              icon="pi pi-link"
              label="Unlink Dynamic TS2"
              style="margin-left: 0.5em"
              @click="unlink(2, slotProps.data)"
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
              class="p-button-raised p-button-rounded p-button-danger"
              icon="pi pi-trash"
              label="Delete"
              style="margin-left: 0.5em"
              v-if="!slotProps.data.editable"
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
import MultiSelect from "primevue/multiselect/sfc";
import Dropdown from "primevue/dropdown/sfc";
import Row from "primevue/row/sfc";
import API from "@/services/API";
import moment from "moment";
import Chip from "primevue/chip/sfc";

import { mapStores } from "pinia";
import { useSettingsStore } from "@/store";

export default {
  components: {
    Button,
    Card,
    Checkbox,
    Dropdown,
    DataTable,
    MultiSelect,
    Column,
    ColumnGroup,
    Row,
    Chip,
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
      repeaters: [],
      expandedRows: [],
      refresh: null,
      talkgroups: [],
      editableRepeaters: 0,
    };
  },
  methods: {
    fetchData() {
      API.get("/talkgroups")
        .then((res) => {
          this.talkgroups = res.data;
          for (let i = 0; i < this.talkgroups.length; i++) {
            this.talkgroups[i].display =
              this.talkgroups[i].id + " - " + this.talkgroups[i].name;
          }
        })
        .catch((err) => {
          console.log(err);
        });

      if (!this.editableRepeaters > 0) {
        API.get("/repeaters/my")
          .then((res) => {
            this.repeaters = res.data;
            for (let i = 0; i < this.repeaters.length; i++) {
              this.repeaters[i].connected_time = moment(
                this.repeaters[i].connected_time
              );

              this.repeaters[i].created_at = moment(
                this.repeaters[i].created_at
              );

              this.repeaters[i].last_ping_time = moment(
                this.repeaters[i].last_ping_time
              );
              this.repeaters[i].editable = false;

              for (
                let j = 0;
                j < this.repeaters[i].ts1_static_talkgroups.length;
                j++
              ) {
                this.repeaters[i].ts1_static_talkgroups[j].display =
                  this.repeaters[i].ts1_static_talkgroups[j].id +
                  " - " +
                  this.repeaters[i].ts1_static_talkgroups[j].name;
              }

              for (
                let j = 0;
                j < this.repeaters[i].ts2_static_talkgroups.length;
                j++
              ) {
                this.repeaters[i].ts2_static_talkgroups[j].display =
                  this.repeaters[i].ts2_static_talkgroups[j].id +
                  " - " +
                  this.repeaters[i].ts2_static_talkgroups[j].name;
              }

              this.repeaters[i].ts1_dynamic_talkgroup.display =
                this.repeaters[i].ts1_dynamic_talkgroup.id +
                " - " +
                this.repeaters[i].ts1_dynamic_talkgroup.name;

              this.repeaters[i].ts2_dynamic_talkgroup.display =
                this.repeaters[i].ts2_dynamic_talkgroup.id +
                " - " +
                this.repeaters[i].ts2_dynamic_talkgroup.name;
            }
          })
          .catch((err) => {
            console.error(err);
          });
      }
    },
    startEdit(repeater) {
      repeater.editable = true;
      this.editableRepeaters++;
    },
    cancelEdit(repeater) {
      repeater.editable = false;
      this.editableRepeaters--;
      if (this.editableRepeaters == 0) {
        this.fetchData();
      }
    },
    saveTalkgroups(repeater) {
      API.post(`/repeaters/${repeater.id}/talkgroups`, {
        ts1_dynamic_talkgroup: repeater.ts1_dynamic_talkgroup,
        ts2_dynamic_talkgroup: repeater.ts2_dynamic_talkgroup,
        ts1_static_talkgroups: repeater.ts1_static_talkgroups,
        ts2_static_talkgroups: repeater.ts2_static_talkgroups,
      })
        .then((res) => {
          this.$toast.add({
            severity: "success",
            summary: "Success",
            detail: `Talkgroups updated for repeater ${repeater.id}`,
            life: 3000,
          });
          repeater.editable = false;
          this.editableRepeaters--;
          if (this.editableRepeaters == 0) {
            this.fetchData();
          }
        })
        .catch((err) => {
          console.error(err);
          if (err.response.data.error) {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: "Failed to update repeater: " + err.response.data.error,
              life: 3000,
            });
            return;
          }
          this.$toast.add({
            severity: "error",
            summary: "Error",
            detail: `Error updating talkgroups for repeater ${repeater.id}`,
            life: 3000,
          });
        });
    },
    unlink(ts, repeater) {
      // API call: POST /repeaters/:id/unlink/dynamic/:ts/:tg
      var tg = 0;
      if (ts == 1) {
        tg = repeater.ts1_dynamic_talkgroup.id;
      } else if (ts == 2) {
        tg = repeater.ts2_dynamic_talkgroup.id;
      }
      API.post(`/repeaters/${repeater.id}/unlink/dynamic/${ts}/${tg}`, {})
        .then((res) => {
          this.$toast.add({
            severity: "success",
            summary: "Success",
            detail: `Talkgroup ${tg} unlinked on TS${ts} for repeater ${repeater.id}`,
            life: 3000,
          });
          this.fetchData();
        })
        .catch((err) => {
          console.error(err);
          this.$toast.add({
            severity: "error",
            summary: "Error",
            detail: `Error unlinking talkgroup for repeater ${repeater.id}`,
            life: 3000,
          });
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
            .then((_res) => {
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
                severity: "error",
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

.chips:not(:first-child) {
  margin-left: 0.5em;
}

.chips .p-chip {
  margin-top: 0.25em;
}
</style>
