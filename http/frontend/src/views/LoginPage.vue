<template>
  <div>
    <Card>
      <template #title>Login</template>
      <template #content>
        <span class="p-float-label">
          <InputText id="username" type="text" v-model="username" />
          <label for="username">Username</label>
        </span>
        <br />
        <span class="p-float-label">
          <InputText id="password" type="password" v-model="password" />
          <label for="password">Password</label>
        </span>
        <br />
        <p>
          If you don't have an account,
          <router-link to="/register">Register here</router-link>
        </p>
      </template>
      <template #footer>
        <div class="card-footer">
          <Button
            class="p-button-raised p-button-rounded"
            icon="pi pi-lock"
            label="Login"
            @click="handleLogin()"
          />
        </div>
      </template>
    </Card>
  </div>
</template>

<script>
import Checkbox from "primevue/checkbox/sfc";
import InputText from "primevue/inputtext/sfc";
import Button from "primevue/button/sfc";
import Card from "primevue/card/sfc";
import API from "@/services/API";

export default {
  components: {
    Checkbox,
    InputText,
    Button,
    Card,
  },
  created() {},
  mounted() {},
  data: function () {
    return {
      username: "",
      password: "",
    };
  },
  methods: {
    handleLogin() {
      API.post("/auth/login", {
        username: this.username,
        password: this.password,
      })
        .then((_res) => {
          this.$router.push("/");
        })
        .catch((err) => {
          console.error(err);
        });
    },
  },
};
</script>

<style scoped></style>
