<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023-2024 Jacob McSwain

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
              <small class="p-error">{{ error.$message.replace("Value", "Radio ID") }}</small>
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
import Card from 'primevue/card';
import Button from 'primevue/button';
import InputText from 'primevue/inputtext';
import API from '@/services/API';

import { useVuelidate } from '@vuelidate/core';
import { required, numeric } from '@vuelidate/validators';

export default {
  components: {
    Card,
    PVButton: Button,
    InputText,
  },
  head: {
    title: 'New Repeater',
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {},
  data: function() {
    return {
      radioID: '',
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

      const numericID = parseInt(this.radioID);
      if (!numericID) {
        return;
      }
      API.post('/repeaters', {
        id: numericID,
      })
        .then((res) => {
          if (!res.data) {
            this.$toast.add({
              summary: 'Error',
              severity: 'error',
              detail: `Error registering repeater`,
              life: 3000,
            });
          } else {
            this.$confirm.require({
              message: res.data.password,
              header: 'Repeater Created',
              acceptClass: 'p-button-success',
              rejectClass: 'remove-reject-button',
              acceptLabel: 'OK',
              accept: () => {
                this.$router.push('/repeaters');
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
              detail: 'Error deleting repeater',
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
