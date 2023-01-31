<template>
  <DataTable
    :value="lastheard"
    :lazy="true"
    :paginator="true"
    :rows="10"
    :totalRecords="totalRecords"
    :loading="loading"
    @page="onPage($event)"
  >
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
      <template #body="slotProps">{{ slotProps.data.duration }}s</template>
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

<script>
import DataTable from "primevue/datatable/sfc";
import Column from "primevue/column/sfc";

import moment from "moment";

import { getWebsocketURI } from "@/services/util";
import API from "@/services/API";

export default {
  name: "LastHeardTable",
  props: {},
  components: {
    DataTable,
    Column,
  },
  data: function () {
    return {
      lastheard: [],
      totalRecords: 0,
      socket: null,
      loading: false,
    };
  },
  mounted() {
    this.fetchData();
    this.socket = new WebSocket(getWebsocketURI() + "/calls");
    this.mapSocketEvents();
  },
  computed: {},
  methods: {
    onPage(event) {
      this.loading = true;
      this.fetchData(event.page + 1, event.rows);
    },
    fetchData(page = 1, limit = 10) {
      API.get(`/lastheard?page=${page}&limit=${limit}`)
        .then((res) => {
          this.totalRecords = res.data.total;
          this.lastheard = this.cleanData(res.data.calls);
          this.loading = false;
        })
        .catch((err) => {
          console.error(err);
        });
    },
    cleanData(data) {
      let copyData = JSON.parse(JSON.stringify(data));
      for (let i = 0; i < copyData.length; i++) {
        copyData[i].start_time = moment(copyData[i].start_time);

        if (typeof copyData[i].duration == "number") {
          copyData[i].duration = (copyData[i].duration / 1000000000).toFixed(1);
        }

        // loss is in a ratio, multiply by 100 to get a percentage
        if (typeof copyData[i].loss == "number") {
          copyData[i].loss = (copyData[i].loss * 100).toFixed(1);
        }

        if (typeof copyData[i].ber == "number") {
          copyData[i].ber = (copyData[i].ber * 100).toFixed(1);
        }

        if (typeof copyData[i].jitter == "number") {
          copyData[i].jitter = copyData[i].jitter.toFixed(1);
        }

        if (typeof copyData[i].rssi == "number") {
          copyData[i].rssi = copyData[i].rssi.toFixed(0);
        }
      }

      return copyData;
    },
    mapSocketEvents() {
      this.socket.addEventListener("open", (event) => {
        console.log("Connected to calls websocket");
      });

      this.socket.addEventListener("close", (event) => {
        console.error("Disconnected from calls websocket");
        console.error("Sleeping for 1 second before reconnecting");
        setTimeout(() => {
          this.socket = new WebSocket(getWebsocketURI() + "/calls");
          this.mapSocketEvents();
        }, 1000);
      });

      this.socket.addEventListener("error", (event) => {
        console.error("Error from calls websocket", event);
        this.socket.close();
        this.socket = new WebSocket(getWebsocketURI() + "/calls");
        this.mapSocketEvents();
      });

      this.socket.addEventListener("message", (event) => {
        const call = JSON.parse(event.data);
        // We need to check that the call is not already in the table
        // If it is, we need to update it
        // If it isn't, we need to add it
        let found = false;
        let copyLastheard = JSON.parse(JSON.stringify(this.lastheard));

        for (let i = 0; i < copyLastheard.length; i++) {
          if (copyLastheard[i].id == call.id) {
            found = true;
            copyLastheard[i] = call;
            break;
          }
        }

        if (!found && copyLastheard.length == 10) {
          copyLastheard.pop();
        }

        if (!found && copyLastheard.length < 10) {
          copyLastheard.unshift(call);
        }

        this.lastheard = this.cleanData(copyLastheard);
      });
    },
  },
};
</script>

<style scoped></style>
