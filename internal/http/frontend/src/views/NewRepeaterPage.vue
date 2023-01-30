<template>
  <div>
    <Toast />
    <ConfirmDialog>
      <template #message="slotProps">
        <div class="flex p-4">
          <p>
            You will need to use this DMRGateway configuration to connect to the
            network.
            <span style="color: red"
              >Save this now, as you will not be able to retrieve it
              again.</span
            >
            <br /><br />
          </p>
          <pre style="background-color: #444; padding: 1em; font-size: 12px">
[DMR Network 2]
Name=AREDN
Enabled=1
Address={{ this.hostname }}
Port=62031
Password="{{ slotProps.message.message }}"
Id={{ this.radioID }}
Location=1
Debug=0
</pre
          >
        </div>
      </template>
    </ConfirmDialog>
    <form @submit.prevent="handleRepeater(!v$.$invalid)">
      <Card>
        <template #title>New Repeater</template>
        <template #content>
          <span class="p-float-label">
            <InputText
              id="radioID"
              type="text"
              v-model="v$.radioID.$model"
              :class="{
                'p-invalid': v$.radioID.$invalid && submitted,
              }"
            />
            <label
              for="radioID"
              :class="{ 'p-error': v$.radioID.$invalid && submitted }"
              >DMR Radio ID</label
            >
          </span>
          <span v-if="v$.radioID.$error && submitted">
            <span v-for="(error, index) of v$.radioID.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
            </span>
            <br />
          </span>
          <span v-else>
            <small
              v-if="
                (v$.radioID.$invalid && submitted) ||
                v$.radioID.$pending.$response
              "
              class="p-error"
              >{{
                v$.radioID.required.$message.replace("Value", "Radio ID")
              }}</small
            >
          </span>
        </template>
        <template #footer>
          <div class="card-footer">
            <Button
              class="p-button-raised p-button-rounded"
              icon="pi pi-save"
              label="Save"
              type="submit"
              @click="handleRepeater(!v$.$invalid)"
            />
          </div>
        </template>
      </Card>
    </form>
  </div>
</template>

<script>
import Card from "primevue/card/sfc";
import Checkbox from "primevue/checkbox/sfc";
import Button from "primevue/button/sfc";
import InputText from "primevue/inputtext/sfc";
import API from "@/services/API";

import { useVuelidate } from "@vuelidate/core";
import { required, numeric } from "@vuelidate/validators";

export default {
  components: {
    Card,
    Checkbox,
    Button,
    InputText,
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {},
  data: function () {
    return {
      radioID: "",
      submitted: false,
      hostname: window.location.hostname,
    };
  },
  validations() {
    return {
      radioID: {
        required,
        numeric,
      },
    };
  },
  methods: {
    handleRepeater(isFormValid) {
      this.submitted = true;
      if (!isFormValid) {
        return;
      }

      var numericID = parseInt(this.radioID);
      if (!numericID) {
        return;
      }
      API.post("/repeaters", {
        id: numericID,
        password: this.repeater_password,
      })
        .then((res) => {
          if (!res.data) {
            this.$toast.add({
              summary: "Error",
              severity: "error",
              detail: `Error registering repeater`,
              life: 3000,
            });
          } else {
            this.$confirm.require({
              message: res.data.password,
              header: "Repeater Created",
              acceptClass: "p-button-success",
              rejectClass: "remove-reject-button",
              acceptLabel: "OK",
              accept: () => {
                this.$router.push("/repeaters");
              },
            });
          }
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
              detail: `Error deleting repeater`,
              life: 3000,
            });
          }
        });
    },
  },
};
</script>

<style>
.remove-reject-button,
.p-dialog-header-close {
  display: none !important;
}
</style>

<style scoped></style>
