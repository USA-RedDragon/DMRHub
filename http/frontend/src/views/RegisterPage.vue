<template>
  <div>
    <Toast />
    <Card>
      <template #title>Register</template>
      <template #content>
        <span class="p-float-label">
          <InputText id="dmr_id" type="text" v-model="dmr_id" />
          <label for="dmr_id">DMR ID</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="username" type="text" v-model="username" />
          <label for="username">Username</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="callsign" type="text" v-model="callsign" />
          <label for="callsign">Callsign</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="password" type="password" v-model="password" />
          <label for="password">Password</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText
            id="confirmPassword"
            type="password"
            v-model="confirmPassword"
          />
          <label for="confirmPassword">Confirm Password</label>
        </span>
      </template>
      <template #footer>
        <div class="card-footer">
          <Button
            class="p-button-raised p-button-rounded"
            icon="pi pi-user"
            label="Register"
            @click="handleRegister()"
          />
        </div>
      </template>
    </Card>
  </div>
</template>

<script>
import InputText from "primevue/inputtext/sfc";
import Button from "primevue/button/sfc";
import Card from "primevue/card/sfc";
import API from "@/services/API";

export default {
  components: {
    InputText,
    Button,
    Card,
  },
  created() {},
  mounted() {},
  data: function () {
    return {
      dmr_id: "",
      username: "",
      callsign: "",
      password: "",
      confirmPassword: "",
    };
  },
  methods: {
    handleRegister() {
      var numericID = parseInt(this.dmr_id);
      if (!numericID) {
        this.$toast.add({
          severity: "error",
          summary: "Error",
          detail: `DMR ID must be a number`,
          life: 3000,
        });
        return;
      }
      if (this.confirmPassword != this.password) {
        this.$toast.add({
          severity: "error",
          summary: "Error",
          detail: `Passwords do not match`,
          life: 3000,
        });
        return;
      }
      API.post("/users", {
        id: numericID,
        callsign: this.callsign,
        username: this.username,
        password: this.password,
      })
        .then((res) => {
          this.$toast.add({
            severity: "success",
            summary: "Success",
            detail: res.data.message,
            life: 3000,
          });
          setTimeout(() => {
            this.$router.push("/");
          }, 3000);
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data) {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: err.response.data.message,
              life: 3000,
            });
          } else {
            this.$toast.add({
              severity: "error",
              summary: "Error",
              detail: "An unknown error occurred",
              life: 3000,
            });
          }
        });
    },
  },
};
</script>

<style scoped></style>
