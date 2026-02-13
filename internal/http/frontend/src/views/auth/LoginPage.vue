<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2026 Jacob McSwain

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program. If not, see <https://www.gnu.org/licenses/>.

  The source code is available at <https://github.com/USA-RedDragon/DMRHub>
-->

<template>
  <div>
    <form @submit.prevent="handleLogin(!v$.$invalid)">
      <Card>
        <CardHeader>
          <CardTitle>Login</CardTitle>
        </CardHeader>
        <CardContent>
          <label class="field-label" for="username">Username</label>
          <ShadInput
            id="username"
            type="text"
            v-model="v$.username.$model"
            :aria-invalid="v$.username.$invalid && submitted"
          />
          <span v-if="v$.username.$error && submitted">
            <span v-for="(error, index) of v$.username.$errors" :key="index">
              <small class="field-error">{{ error.$message.replace("Value", "Username") }}</small>
            </span>
            <br />
          </span>
          <br />
          <label class="field-label" for="password">Password</label>
          <ShadInput
            id="password"
            type="password"
            v-model="v$.password.$model"
            :aria-invalid="v$.password.$invalid && submitted"
          />
          <span v-if="v$.password.$error && submitted">
            <span v-for="(error, index) of v$.password.$errors" :key="index">
              <small class="field-error">{{ error.$message.replace("Value", "Password") }}</small>
            </span>
            <br />
          </span>
          <br />
          <p>
            If you don't have an account,
            <router-link to="/register">Register here</router-link>
          </p>
        </CardContent>
        <CardFooter>
          <div class="card-footer">
            <ShadButton type="submit" variant="outline" size="sm">Login</ShadButton>
          </div>
        </CardFooter>
      </Card>
    </form>
  </div>
</template>

<script lang="ts">
import API from '@/services/API';
import { Button as ShadButton } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';

import { useVuelidate } from '@vuelidate/core';
import { required } from '@vuelidate/validators';

import { mapStores } from 'pinia';
import { useUserStore } from '@/store';

export default {
  components: {
    ShadButton,
    Card,
    CardContent,
    CardFooter,
    CardHeader,
    CardTitle,
    ShadInput,
  },
  head: {
    title: 'Login',
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
    handleLogin(isFormValid: boolean) {
      this.submitted = true;
      if (!isFormValid) {
        return;
      }

      API.post('/auth/login', {
        username: this.username.trim(),
        password: this.password.trim(),
      })
        .then(() => {
          API.get('/users/me').then((res) => {
            this.userStore.id = res.data.id;
            this.userStore.callsign = res.data.callsign;
            this.userStore.username = res.data.username;
            this.userStore.admin = res.data.admin;
            this.userStore.superAdmin = res.data.superAdmin;
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

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}

.field-error {
  color: hsl(var(--primary));
}
</style>
