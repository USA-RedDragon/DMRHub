<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https:  www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <div>
    <PVToast />
    <form @submit.prevent="handleLogin(!v$.$invalid)">
      <Card>
        <template #title>Login</template>
        <template #content>
          <span class="p-float-label">
            <InputText
              id="username"
              type="text"
              v-model="v$.username.$model"
              :class="{
                'p-invalid': v$.username.$invalid && submitted,
              }"
            />
            <label
              for="username"
              :class="{ 'p-error': v$.username.$invalid && submitted }"
              >Username</label
            >
          </span>
          <span v-if="v$.username.$error && submitted">
            <span v-for="(error, index) of v$.username.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
            </span>
            <br />
          </span>
          <br />
          <span class="p-float-label">
            <InputText
              id="password"
              type="password"
              v-model="v$.password.$model"
              :class="{
                'p-invalid': v$.password.$invalid && submitted,
              }"
            />
            <label
              for="password"
              :class="{ 'p-error': v$.password.$invalid && submitted }"
              >Password</label
            >
          </span>
          <span v-if="v$.password.$error && submitted">
            <span v-for="(error, index) of v$.password.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
            </span>
            <br />
          </span>
          <br />
          <p>
            If you don't have an account,
            <router-link to="/register">Register here</router-link>
          </p>
        </template>
        <template #footer>
          <div class="card-footer">
            <PVButton
              class="p-button-raised p-button-rounded"
              icon="pi pi-lock"
              label="Login"
              type="submit"
            />
          </div>
        </template>
      </Card>
    </form>
  </div>
</template>

<script>
import InputText from 'primevue/inputtext/sfc';
import Button from 'primevue/button/sfc';
import Card from 'primevue/card/sfc';
import API from '@/services/API';

import { useVuelidate } from '@vuelidate/core';
import { required } from '@vuelidate/validators';

import { mapStores } from 'pinia';
import { useUserStore } from '@/store';

export default {
  components: {
    InputText,
    PVButton: Button,
    Card,
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {},
  data: function() {
    return {
      username: '',
      password: '',
      submitted: false,
    };
  },
  validations() {
    return {
      username: {
        required,
      },
      password: {
        required,
      },
    };
  },
  methods: {
    handleLogin(isFormValid) {
      this.submitted = true;
      if (!isFormValid) {
        return;
      }

      API.post('/auth/login', {
        username: this.username.trim(),
        password: this.password.trim(),
      })
        .then((_res) => {
          API.get('/users/me').then((res) => {
            this.userStore.id = res.data.id;
            this.userStore.callsign = res.data.callsign;
            this.userStore.username = res.data.username;
            this.userStore.admin = res.data.admin;
            this.userStore.created_at = res.data.created_at;
            this.userStore.loggedIn = true;
            this.$router.push('/');
          });
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              summary: 'Error',
              severity: 'error',
              detail: err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              summary: 'Error',
              severity: 'error',
              detail: `Error logging in`,
              life: 3000,
            });
          }
        });
    },
  },
  computed: {
    ...mapStores(useUserStore),
  },
};
</script>

<style scoped></style>
