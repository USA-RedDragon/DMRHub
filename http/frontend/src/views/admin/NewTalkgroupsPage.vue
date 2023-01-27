<template>
  <div>
    <Toast />
    <Card>
      <template #title>New Talkgroup</template>
      <template #content>
        <span class="p-float-label">
          <InputText id="id" type="text" v-model="id" />
          <label for="id">Talkgroup ID</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="name" type="text" v-model="name" />
          <label for="name">Name</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="description" type="text" v-model="description" />
          <label for="description">Description</label>
        </span>
        <br />
        <span class="p-float-label">
          <MultiSelect
            id="admins"
            v-model="admins"
            :options="allUsers"
            :filter="true"
            optionLabel="display"
            display="chip"
            style="width: 100%"
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
        <br />
        <span class="p-float-label">
          <MultiSelect
            id="ncos"
            v-model="ncos"
            :options="allUsers"
            :filter="true"
            optionLabel="display"
            display="chip"
            style="width: 100%"
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
      <template #footer>
        <div class="card-footer">
          <Button
            class="p-button-raised p-button-rounded"
            icon="pi pi-save"
            label="Save"
            @click="handleTalkgroup()"
          />
        </div>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import Checkbox from "primevue/checkbox/sfc";
import Button from "primevue/button/sfc";
import InputText from "primevue/inputtext/sfc";
import MultiSelect from "primevue/multiselect/sfc";
import API from "@/services/API";

export default {
  components: {
    Card,
    Checkbox,
    Button,
    InputText,
    MultiSelect,
  },
  created() {},
  mounted() {
    this.getData();
  },
  data: function () {
    return {
      id: "",
      name: "",
      description: "",
      admins: [],
      ncos: [],
      allUsers: [],
    };
  },
  methods: {
    getData() {
      API.get("/users")
        .then((res) => {
          this.allUsers = res.data;
          var parrotIndex = -1;
          for (let i = 0; i < this.allUsers.length; i++) {
            this.allUsers[
              i
            ].display = `${this.allUsers[i].id} - ${this.allUsers[i].callsign}`;
            // Remove user with id 9990 (parrot)
            if (this.allUsers[i].id === 9990) {
              parrotIndex = i;
            }
          }
          if (parrotIndex !== -1) {
            this.allUsers.splice(parrotIndex, 1);
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
    handleTalkgroup() {
      var numericID = parseInt(this.id);
      if (!numericID) {
        return;
      }
      API.post("/talkgroups", {
        id: numericID,
        name: this.name,
        description: this.description,
      })
        .then((_res) => {
          for (var i = 0; i < this.admins.length; i++) {
            API.post(`/talkgroups/${numericID}/appoint`, {
              user_id: this.admins[i].id,
            }).catch((err) => {
              console.error(err);
              if (
                err.response &&
                err.response.data &&
                err.response.data.error
              ) {
                this.$toast.add({
                  summary: "Error",
                  severity: "error",
                  detail: err.response.data.error,
                  life: 3000,
                });
              } else {
                this.$toast.add({
                  summary: "Error",
                  severity: "error",
                  detail: `Error creating talkgroup`,
                  life: 3000,
                });
              }
            });
          }
          for (i = 0; i < this.ncos.length; i++) {
            API.post(`/talkgroups/${numericID}/nco`, {
              user_id: this.ncos[i].id,
            }).catch((err) => {
              console.error(err);
              if (
                err.response &&
                err.response.data &&
                err.response.data.error
              ) {
                this.$toast.add({
                  summary: "Error",
                  severity: "error",
                  detail: err.response.data.error,
                  life: 3000,
                });
              } else {
                this.$toast.add({
                  summary: "Error",
                  severity: "error",
                  detail: `Error creating talkgroup`,
                  life: 3000,
                });
              }
            });
          }
          // Now show a toast for a few seconds before redirecting to /admin/talkgroups
          this.$toast.add({
            summary: "Success",
            severity: "success",
            detail: `Talkgroup created, redirecting...`,
            life: 3000,
          });
          setTimeout(() => {
            this.$router.push("/admin/talkgroups");
          }, 3000);
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              summary: "Error",
              severity: "error",
              detail: err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              summary: "Error",
              severity: "error",
              detail: `Error creating talkgroup`,
              life: 3000,
            });
          }
        });
    },
  },
};
</script>

<style scoped></style>
