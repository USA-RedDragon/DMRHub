<template>
  <div>
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
          <InputText id="admins" type="text" v-model="admins" />
          <label for="admins">Admins</label>
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
import API from "@/services/API";

export default {
  components: {
    Card,
    Checkbox,
    Button,
    InputText,
  },
  created() {},
  mounted() {},
  data: function () {
    return {
      id: "",
      name: "",
      description: "",
      admins: [],
    };
  },
  methods: {
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
              user_id: this.admins[i],
            }).catch((err) => {
              console.error(err);
            });
          }

          this.$router.push("/admin/talkgroups");
        })
        .catch((err) => {
          console.error(err);
        });
    },
  },
};
</script>

<style scoped></style>
