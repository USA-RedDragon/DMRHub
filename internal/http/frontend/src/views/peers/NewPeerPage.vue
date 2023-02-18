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
    <Toast />
    <ConfirmDialog>
      <template #message="slotProps">
        <div class="flex p-4">
          <p>
            You will need to use this password configuration to connect to the
            network.
            <br /><span style="color: red"
              >Save this now, as you will not be able to retrieve it
              again.</span
            >
            <br />Your Peer password is:
            <code style="color: orange">{{ slotProps.message.message }}</code>
          </p>
        </div>
      </template>
    </ConfirmDialog>
    <form @submit.prevent="handlePeer(!v$.$invalid)">
      <Card>
        <template #title>New Peer</template>
        <template #content>
          <span class="p-float-label">
            <InputText
              id="id"
              type="text"
              v-model="v$.id.$model"
              :class="{
                'p-invalid': v$.id.$invalid && submitted,
              }"
            />
            <label for="id" :class="{ 'p-error': v$.id.$invalid && submitted }"
              >Peer ID</label
            >
          </span>
          <span v-if="v$.id.$error && submitted">
            <span v-for="(error, index) of v$.id.$errors" :key="index">
              <small class="p-error">{{ error.$message }}</small>
            </span>
            <br />
          </span>
          <span v-else>
            <small
              v-if="(v$.id.$invalid && submitted) || v$.id.$pending.$response"
              class="p-error"
              >{{ v$.id.required.$message.replace("Value", "Peer ID") }}</small
            >
          </span>
          <br />
          <span>
            <Checkbox
              id="ingress"
              inputId="ingress"
              v-model="ingress"
              :binary="true"
            />
            <label for="ingress"
              >&nbsp;&nbsp;Peer should receive traffic from the server?</label
            >
          </span>
          <br />
          <br />
          <span>
            <Checkbox
              id="egress"
              inputId="egress"
              v-model="egress"
              :binary="true"
            />
            <label for="egress"
              >&nbsp;&nbsp;Peer should send traffic to the server?</label
            >
          </span>
        </template>
        <template #footer>
          <div class="card-footer">
            <PVButton
              class="p-button-raised p-button-rounded"
              icon="pi pi-save"
              label="Save"
              type="submit"
            />
          </div>
        </template>
      </Card>
    </form>
  </div>
</template>

<script>
import Card from 'primevue/card/sfc';
import Checkbox from 'primevue/checkbox/sfc';
import Button from 'primevue/button/sfc';
import InputText from 'primevue/inputtext/sfc';
import API from '@/services/API';

import { useVuelidate } from '@vuelidate/core';
import { required, numeric } from '@vuelidate/validators';

export default {
  components: {
    Card,
    Checkbox,
    PVButton: Button,
    InputText,
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {},
  data: function() {
    return {
      id: '',
      ingress: false,
      egress: false,
      submitted: false,
      hostname: window.location.hostname,
    };
  },
  validations() {
    return {
      id: {
        required,
        numeric,
      },
    };
  },
  methods: {
    handlePeer(isFormValid) {
      this.submitted = true;
      if (!isFormValid) {
        return;
      }

      const numericID = parseInt(this.id);
      if (!numericID) {
        return;
      }
      API.post('/peers', {
        id: numericID,
        ingress: this.ingress,
        egress: this.egress,
      })
        .then((res) => {
          if (!res.data) {
            this.$toast.add({
              summary: 'Error',
              severity: 'error',
              detail: `Error registering peer`,
              life: 3000,
            });
          } else {
            this.$confirm.require({
              message: res.data.password,
              header: 'Peer Created',
              acceptClass: 'p-button-success',
              rejectClass: 'remove-reject-button',
              acceptLabel: 'OK',
              accept: () => {
                this.$router.push('/repeaters/peers');
              },
            });
          }
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
              detail: `Error creating peer`,
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
