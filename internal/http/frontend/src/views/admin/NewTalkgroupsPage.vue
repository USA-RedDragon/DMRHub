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
    <form @submit.prevent="handleTalkgroup(!v$.$invalid)">
      <Card>
        <CardHeader>
          <CardTitle>New Talkgroup</CardTitle>
        </CardHeader>
        <CardContent>
          <label class="field-label" for="id">Talkgroup ID</label>
          <ShadInput
            id="id"
            type="text"
            v-model="v$.id.$model"
            :aria-invalid="v$.id.$invalid && submitted"
          />
          <span v-if="v$.id.$error && submitted">
            <span v-for="(error, index) of v$.id.$errors" :key="index">
              <small class="p-error">{{ error.$message.replace("Value", "ID") }}</small>
            </span>
          </span>
          <span v-else>
            <small
              v-if="(v$.id.$invalid && submitted) || v$.id.$pending.$response"
              class="p-error"
              >{{ v$.id.required.$message.replace("Value", "ID") }}
              <br />
            </small>
          </span>
          <br />
          <label class="field-label" for="name">Name</label>
          <ShadInput
            id="name"
            type="text"
            v-model="v$.name.$model"
            :aria-invalid="v$.name.$invalid && submitted"
          />
          <span v-if="v$.name.$error && submitted">
            <span v-for="(error, index) of v$.name.$errors" :key="index">
              <small class="p-error">{{ error.$message.replace("Value", "Name") }}</small>
              <br />
            </span>
          </span>
          <span v-else>
            <small
              v-if="
                (v$.name.$invalid && submitted) || v$.name.$pending.$response
              "
              class="p-error"
              >{{ v$.name.required.$message.replace("Value", "Name") }}
              <br />
            </small>
          </span>
          <br />
          <label class="field-label" for="description">Description</label>
          <ShadInput
            id="description"
            type="text"
            v-model="v$.description.$model"
            :aria-invalid="v$.description.$invalid && submitted"
          />
          <span v-if="v$.description.$error && submitted">
            <span v-for="(error, index) of v$.description.$errors" :key="index">
              <small class="p-error">{{ error.$message.replace("Value", "Description") }}</small>
            </span>
            <br />
          </span>
          <span v-else>
            <small
              v-if="
                (v$.description.$invalid && submitted) ||
                v$.description.$pending.$response
              "
              class="p-error"
              >{{
                v$.description.required.$message.replace("Value", "Description")
              }}
              <br />
            </small>
          </span>
          <br />
          <label class="field-label" for="admins">Admins</label>
          <span>
            <select
              id="admins"
              v-model="admins"
              class="ui-select-multiple"
              multiple
            >
              <option v-for="user in allUsers" :key="user.id" :value="user.id">{{ user.display }}</option>
            </select>
          </span>
          <br />
          <label class="field-label" for="ncos">Net Control Operators</label>
          <span>
            <select
              id="ncos"
              v-model="ncos"
              class="ui-select-multiple"
              multiple
            >
              <option v-for="user in allUsers" :key="user.id" :value="user.id">{{ user.display }}</option>
            </select>
          </span>
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
import { required, numeric, maxLength } from '@vuelidate/validators';

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
    title: 'New Talkgroup',
    titleTemplate: 'Admin | %s | ' + (localStorage.getItem('title') || 'DMRHub'),
  },
  setup: () => ({ v$: useVuelidate() }),
  created() {},
  mounted() {
    this.getData();
  },
  data: function() {
    return {
      id: '',
      name: '',
      description: '',
      admins: [] as number[],
      ncos: [] as number[],
      allUsers: [] as Array<{ id: number; callsign: string; display?: string }>,
      submitted: false,
    };
  },
  validations() {
    return {
      id: {
        required,
        numeric,
      },
      name: {
        required,
        maxLength: maxLength(20),
      },
      description: {
        required,
        maxLength: maxLength(240),
      },
      ncos: {},
      admins: {},
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
    handleTalkgroup(isFormValid: boolean) {
      this.submitted = true;

      if (!isFormValid) {
        return;
      }

      const numericID = parseInt(this.id);
      if (!numericID) {
        return;
      }
      API.post('/talkgroups', {
        id: numericID,
        name: this.name.trim(),
        description: this.description.trim(),
      })
        .then(() => {
          API.post(`/talkgroups/${numericID}/admins`, {
            user_ids: this.admins,
          })
            .then(() => {
              API.post(`/talkgroups/${numericID}/ncos`, {
                user_ids: this.ncos,
              })
                .then(() => {
                  // Now show a toast for a few seconds before redirecting to /admin/talkgroups
                  this.$toast.add({
                    summary: 'Success',
                    severity: 'success',
                    detail: `Talkgroup created, redirecting...`,
                    life: 3000,
                  });
                  setTimeout(() => {
                    this.$router.push('/admin/talkgroups');
                  }, 3000);
                })
                .catch((err) => {
                  console.error(err);
                  if (
                    err.response &&
                    err.response.data &&
                    err.response.data.error
                  ) {
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
                      detail: `Error creating talkgroup`,
                      life: 3000,
                    });
                  }
                });
            })
            .catch((err) => {
              console.error(err);
              if (
                err.response &&
                err.response.data &&
                err.response.data.error
              ) {
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
                  detail: `Error creating talkgroup`,
                  life: 3000,
                });
              }
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
              detail: `Error creating talkgroup`,
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

.ui-select-multiple {
  width: 100%;
  min-height: 8rem;
  border: 1px solid var(--border);
  border-radius: 0.5rem;
  background: var(--background);
  color: var(--foreground);
  padding: 0.5rem 0.75rem;
}
</style>
