<template>
  <div>
    <Card>
      <template #title>New Repeater</template>
      <template #content>
        <span class="p-float-label">
          <InputText id="radio_id" type="text" v-model="radio_id" />
          <label for="radio_id">DMR Radio ID</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText
            id="repeater_password"
            type="password"
            v-model="repeater_password"
          />
          <label for="repeater_password">Repeater Password</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText
            id="confirm_repeater_password"
            type="password"
            v-model="confirm_repeater_password"
          />
          <label for="confirm_repeater_password"
            >Confirm Repeater Password</label
          >
        </span>
      </template>
      <template #footer>
        <div class="card-footer">
          <Button
            class="p-button-raised p-button-rounded"
            icon="pi pi-save"
            label="Save"
            @click="handleRepeater()"
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
      radio_id: "",
      repeater_password: "",
      confirm_repeater_password: "",
    };
  },
  methods: {
    handleRepeater() {
      var numericID = parseInt(this.radio_id);
      if (!numericID) {
        return;
      }
      if (this.repeater_password != this.confirm_repeater_password) {
        return;
      }
      API.post("/repeaters", {
        id: numericID,
        password: this.repeater_password,
      })
        .then((_res) => {
          this.$router.push("/repeaters");
        })
        .catch((err) => {
          console.error(err);
        });
    },
  },
};
</script>

<style scoped></style>
