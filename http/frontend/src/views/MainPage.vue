<template>
  <div>
    <Card>
      <template #title>Last Heard</template>
      <template #content>
        <DataTable :value="lastheard">
          <Column field="start_time" header="Time">
            <template #body="slotProps">
              <span v-if="!slotProps.data.active">{{
                slotProps.data.start_time.fromNow()
              }}</span>
              <span v-else>Active</span>
            </template>
          </Column>
          <Column field="time_slot" header="TS">
            <template #body="slotProps">
              <span v-if="slotProps.data.time_slot">2</span>
              <span v-else>1</span>
            </template>
          </Column>
          <Column field="user" header="User">
            <template #body="slotProps">
              {{ slotProps.data.user.callsign }} |
              {{ slotProps.data.user.id }}
            </template>
          </Column>
          <Column field="destination_id" header="Destination">
            <template #body="slotProps">
              <span v-if="slotProps.data.is_to_talkgroup">
                TG {{ slotProps.data.to_talkgroup.id }}
              </span>
              <span v-if="slotProps.data.is_to_repeater">
                {{ slotProps.data.to_repeater.callsign }} |
                {{ slotProps.data.to_repeater.radio_id }}
              </span>
              <span v-if="slotProps.data.is_to_user">
                {{ slotProps.data.to_user.callsign }} |
                {{ slotProps.data.to_user.id }}
              </span>
            </template>
          </Column>
          <Column field="duration" header="Duration">
            <template #body="slotProps"
              >{{ slotProps.data.duration }}s</template
            >
          </Column>
          <Column field="ber" header="BER">
            <template #body="slotProps">{{ slotProps.data.ber }}%</template>
          </Column>
          <Column field="loss" header="Loss">
            <template #body="slotProps">{{ slotProps.data.loss }}%</template>
          </Column>
          <Column field="jitter" header="Jitter">
            <template #body="slotProps">{{ slotProps.data.jitter }}ms</template>
          </Column>
          <Column field="rssi" header="RSSI">
            <template #body="slotProps">
              <span v-if="slotProps.data.rssi != 0"
                >-{{ slotProps.data.rssi }}dBm</span
              >
              <span v-else>-</span>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import API from "@/services/API";
import moment from "moment";
import DataTable from "primevue/datatable/sfc";
import Column from "primevue/column/sfc";

import { mapStores } from "pinia";
import { useSettingsStore } from "@/store";

export default {
  components: {
    Card,
    DataTable,
    Column,
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
      lastheard: [],
      refresh: null,
    };
  },
  methods: {
    fetchData() {
      API.get("/lastheard")
        .then((res) => {
          this.lastheard = res.data;
          for (let i = 0; i < this.lastheard.length; i++) {
            this.lastheard[i].start_time = moment(this.lastheard[i].start_time);
            // lastheard.duration is in nanoseconds, convert to seconds
            this.lastheard[i].duration = (
              this.lastheard[i].duration / 1000000000
            ).toFixed(1);

            // loss is in a ratio, multiply by 100 to get a percentage
            this.lastheard[i].loss = (this.lastheard[i].loss * 100).toFixed(1);
            this.lastheard[i].ber = (this.lastheard[i].ber * 100).toFixed(1);
            this.lastheard[i].rssi = this.lastheard[i].rssi.toFixed(0);

            this.lastheard[i].jitter = this.lastheard[i].jitter.toFixed(1);
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
  },
  computed: {
    ...mapStores(useSettingsStore),
  },
};
</script>

<style scoped></style>
