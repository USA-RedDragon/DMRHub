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
    <form @submit.prevent="handlePeer(!v$.$invalid)">
      <Card>
        <CardHeader>
          <CardTitle>New Peer</CardTitle>
        </CardHeader>
        <CardContent>
          <label class="field-label" for="owner">Owner</label>
          <select
            id="owner"
            v-model="v$.owner_id.$model"
            class="ui-select"
            :class="{ 'ui-select-invalid': v$.owner_id.$invalid && submitted }"
          >
            <option value="" disabled>Select owner</option>
            <option v-for="user in allUsers" :key="user.id" :value="user.id">{{ user.display }}</option>
          </select>
          <span v-if="v$.owner_id.$error && submitted">
            <span v-for="(error, index) of v$.owner_id.$errors" :key="index">
              <small class="p-error">{{ error.$message.replace("Value", "Owner") }}</small>
            </span>
            <br />
          </span>
          <span v-else>
            <small
              v-if="(v$.owner_id.$invalid && submitted) || v$.owner_id.$pending.$response"
              class="p-error"
              >{{ v$.owner_id.required.$message.replace("Value", "Owner") }}</small
            >
          </span>
          <br />
          <label class="field-label" for="id">Peer ID</label>
          <ShadInput
            id="id"
            type="text"
            v-model="v$.id.$model"
            :aria-invalid="v$.id.$invalid && submitted"
          />
          <span v-if="v$.id.$error && submitted">
            <span v-for="(error, index) of v$.id.$errors" :key="index">
              <small class="p-error">{{ error.$message.replace("Value", "Peer ID") }}</small>
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
          <div class="checkbox-row">
            <input id="ingress" type="checkbox" v-model="ingress" />
            <label for="ingress">Receive DMR traffic from this peer</label>
          </div>
          <br />
          <br />
          <div class="checkbox-row">
            <input id="egress" type="checkbox" v-model="egress" />
            <label for="egress">Transmit DMR traffic to this peer</label>
          </div>
        </CardContent>
        <CardFooter>
          <div class="card-footer">
            <ShadButton type="submit">Save</ShadButton>
          </div>
        </CardFooter>
      </Card>
    </form>
  </div>
</template>

<script lang="ts">
import { Button as ShadButton } from '@/components/ui/button';
import { Input as ShadInput } from '@/components/ui/input';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
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
    title: 'New OpenBridge Peer',
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {
    this.getData();
  },
  mounted() {},
  data: function() {
    return {
      id: '',
      ingress: false,
      egress: false,
      submitted: false,
      hostname: window.location.hostname,
      allUsers: [] as Array<{ id: number; callsign: string; display?: string }>,
      owner_id: '',
    };
  },
  validations() {
    return {
      id: {
        required,
        numeric,
      },
      owner_id: {
        required,
        numeric,
      },
    };
  },
  methods: {
    getData() {
      API.get('/users?limit=none')
        .then((res) => {
          this.allUsers = res.data.users;
          let parrotIndex = -1;
          for (let i = 0; i < this.allUsers.length; i++) {
            const user = this.allUsers[i];
            if (!user) continue;
            user.display = `${user.id} - ${user.callsign}`;
            // Remove user with id 9990 (parrot)
            if (user.id === 9990) {
              parrotIndex = i;
            }
          }
          if (parrotIndex !== -1) {
            this.allUsers.splice(parrotIndex, 1);
          }
        })
        .catch((err) => {
          console.error(err);
        });
    },
    handlePeer(isFormValid: boolean) {
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
        owner: parseInt(this.owner_id),
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
            window.alert(
              'Peer Created\n\n'
              + 'You will need to use this password configuration to connect to the network. '
              + 'Save this now, as you will not be able to retrieve it again.\n\n'
              + `Peer password: ${res.data.password}`,
            );
            this.$router.push('/admin/peers');
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

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}

.ui-select {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}

.ui-select:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 2px;
}

.ui-select-invalid {
  border-color: var(--primary);
}

.checkbox-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
