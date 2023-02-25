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
    <form @submit.prevent="handleRegister(!v$.$invalid)">
      <Card>
        <template #title>Register</template>
        <template #content>
          <span class="p-float-label">
            <InputText
              id="dmr_id"
              type="text"
              v-model="v$.dmr_id.$model"
              :class="{
                'p-invalid': v$.dmr_id.$invalid && submitted,
              }"
            />
            <label
              for="dmr_id"
              :class="{ 'p-error': v$.dmr_id.$invalid && submitted }"
              >DMR ID</label
            >
          </span>
          <span v-if="v$.dmr_id.$error && submitted">
            <span v-for="(error, index) of v$.dmr_id.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.dmr_id.$invalid && submitted) ||
                v$.dmr_id.$pending.$response
              "
              class="p-error"
              >{{ v$.dmr_id.required.$message }}
              <br />
            </small>
          </span>
          <br />
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
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.username.$invalid && submitted) ||
                v$.username.$pending.$response
              "
              class="p-error"
              >{{ v$.username.required.$message }}
              <br />
            </small>
          </span>
          <br />
          <span class="p-float-label">
            <InputText
              id="callsign"
              type="text"
              v-model="v$.callsign.$model"
              :class="{ 'p-invalid': v$.callsign.$invalid && submitted }"
            />
            <label
              for="callsign"
              :class="{ 'p-error': v$.callsign.$invalid && submitted }"
              >Callsign</label
            >
          </span>
          <span v-if="v$.callsign.$error && submitted">
            <span v-for="(error, index) of v$.callsign.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.callsign.$invalid && submitted) ||
                v$.callsign.$pending.$response
              "
              class="p-error"
              >{{ v$.callsign.required.$message }}
              <br />
            </small>
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
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.password.$invalid && submitted) ||
                v$.password.$pending.$response
              "
              class="p-error"
              >{{ v$.password.required.$message }}
              <br />
            </small>
          </span>
          <br />
          <span class="p-float-label">
            <InputText
              id="confirmPassword"
              type="password"
              v-model="v$.confirmPassword.$model"
              :class="{
                'p-invalid': v$.confirmPassword.$invalid && submitted,
              }"
            />
            <label
              for="confirmPassword"
              :class="{ 'p-error': v$.confirmPassword.$invalid && submitted }"
              >Confirm Password</label
            >
          </span>
          <span v-if="v$.confirmPassword.$error && submitted">
            <span
              v-for="(error, index) of v$.confirmPassword.$errors"
              :key="index"
            >
              <small class="p-error">{{ error.$message }}</small>
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.confirmPassword.$invalid && submitted) ||
                v$.confirmPassword.$pending.$response
              "
              class="p-error"
              >{{ v$.confirmPassword.required.$message }}
              <br />
            </small>
          </span>
        </template>
        <template #footer>
          <div class="card-footer">
            <PVButton
              class="p-button-raised p-button-rounded"
              icon="pi pi-user"
              type="submit"
              label="Register"
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
import { required, sameAs, numeric } from '@vuelidate/validators';

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
      dmr_id: '',
      username: '',
      callsign: '',
      password: '',
      confirmPassword: '',
      submitted: false,
    };
  },
  validations() {
    return {
      dmr_id: {
        required,
        numeric,
      },
      username: {
        required,
      },
      callsign: {
        required,
      },
      password: {
        required,
      },
      confirmPassword: {
        required,
        sameAs: sameAs(this.password),
      },
    };
  },
  methods: {
    handleRegister(isFormValid) {
      this.submitted = true;
      if (!isFormValid) {
        return;
      }

      const numericID = parseInt(this.dmr_id);
      if (!numericID) {
        this.$toast.add({
          severity: 'error',
          summary: 'Error',
          detail: `DMR ID must be a number`,
          life: 3000,
        });
        return;
      }
      if (this.confirmPassword != this.password) {
        this.$toast.add({
          severity: 'error',
          summary: 'Error',
          detail: `Passwords do not match`,
          life: 3000,
        });
        return;
      }
      API.post('/users', {
        id: numericID,
        callsign: this.callsign,
        username: this.username,
        password: this.password,
      })
        .then((res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: res.data.message,
            life: 3000,
          });
          setTimeout(() => {
            this.$router.push('/');
          }, 3000);
        })
        .catch((err) => {
          console.error(err);
          if (err.response && err.response.data && err.response.data.error) {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: err.response.data.error,
              life: 3000,
            });
          } else {
            this.$toast.add({
              severity: 'error',
              summary: 'Error',
              detail: 'An unknown error occurred',
              life: 3000,
            });
          }
        });
    },
  },
};
</script>

<style scoped></style>
