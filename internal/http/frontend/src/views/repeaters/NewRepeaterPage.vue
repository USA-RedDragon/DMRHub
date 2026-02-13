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
    <form @submit.prevent="handleRepeater(!v$.$invalid)">
      <Card>
        <CardHeader>
          <CardTitle>New Repeater</CardTitle>
        </CardHeader>
        <CardContent>
          <div class="field">
            <label class="field-label" for="repeaterType">Repeater Type</label>
            <br />
            <select
              id="repeaterType"
              v-model="repeaterType"
              class="ui-select"
            >
              <option v-for="type in repeaterTypes" :key="type.value" :value="type.value">{{ type.label }}</option>
            </select>
          </div>
          <br />
          <label class="field-label" for="radioID">DMR Radio ID</label>
          <ShadInput
            id="radioID"
            type="text"
            v-model="v$.radioID.$model"
            :aria-invalid="v$.radioID.$invalid && submitted"
          />
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
        </CardContent>
        <CardFooter>
          <div class="card-footer">
            <ShadButton type="submit" variant="outline" size="sm">Save</ShadButton>
          </div>
        </CardFooter>
      </Card>
    </form>
  </div>
</template>

<script lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import API from '@/services/API';

import { useVuelidate } from '@vuelidate/core';
import { required, numeric } from '@vuelidate/validators';

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
    title: 'New Repeater',
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {},
  data: function() {
    return {
      radioID: '',
      repeaterType: 'mmdvm',
      repeaterTypes: [
        { label: 'MMDVM', value: 'mmdvm' },
        { label: 'Motorola IPSC', value: 'ipsc' },
      ],
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
    handleRepeater(isFormValid: boolean) {
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
        type: this.repeaterType,
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
            if (this.repeaterType === 'mmdvm') {
              window.alert(
                'Repeater Created\n\n'
                + 'You will need to use this DMRGateway configuration to connect to the network. '
                + 'Save this now, as you will not be able to retrieve it again.\n\n'
                + '[DMR Network 2]\n'
                + 'Name=AREDN\n'
                + 'Enabled=1\n'
                + `Address=${this.hostname}\n`
                + 'Port=62031\n'
                + `Password="${res.data.password}"\n`
                + `Id=${this.radioID}\n`
                + 'Location=1\n'
                + 'Debug=0',
              );
            } else {
              window.alert(
                'Peer Created\n\n'
                + 'Your Motorola IPSC repeater has been created. '
                + 'Save this auth key now, as you will not be able to retrieve it again.\n\n'
                + `Master IP: ${this.hostname}\n`
                + 'Port: 50000\n'
                + `Auth Key: ${res.data.password}`,
              );
            }
            this.$router.push('/repeaters');
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

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}

.ui-select {
  width: 100%;
  border: 1px solid hsl(var(--border));
  border-radius: 0.5rem;
  background: hsl(var(--background));
  color: hsl(var(--foreground));
  padding: 0.5rem 0.75rem;
}

.ui-select:focus-visible {
  outline: 2px solid hsl(var(--primary));
  outline-offset: 2px;
}
</style>
