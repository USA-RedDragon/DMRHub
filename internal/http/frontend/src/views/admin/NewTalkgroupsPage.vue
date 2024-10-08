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
    <form @submit.prevent="handleTalkgroup(!v$.$invalid)">
      <Card>
        <template #title>New Talkgroup</template>
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
              >Talkgroup ID</label
            >
          </span>
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
          <span class="p-float-label">
            <InputText
              id="name"
              type="text"
              v-model="v$.name.$model"
              :class="{
                'p-invalid': v$.name.$invalid && submitted,
              }"
            />
            <label
              for="name"
              :class="{ 'p-error': v$.name.$invalid && submitted }"
              >Name</label
            >
          </span>
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
          <span class="p-float-label">
            <InputText
              id="description"
              type="text"
              v-model="v$.description.$model"
              :class="{
                'p-invalid': v$.description.$invalid && submitted,
              }"
            />
            <label
              for="description"
              :class="{ 'p-error': v$.description.$invalid && submitted }"
              >Description</label
            >
          </span>
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
          <span class="p-float-label">
            <MultiSelect
              id="admins"
              v-model="admins"
              :options="allUsers"
              :filter="true"
              optionLabel="display"
              display="chip"
              style="width: 100%"
            >
              <template #chip="slotProps">
                {{ slotProps.value.display }}
              </template>
              <template #option="slotProps">
                {{ slotProps.option.display }}
              </template>
            </MultiSelect>
            <label for="admins">Admins</label>
          </span>
          <br />
          <span class="p-float-label">
            <MultiSelect
              id="ncos"
              v-model="ncos"
              :options="allUsers"
              :filter="true"
              optionLabel="display"
              display="chip"
              style="width: 100%"
            >
              <template #chip="slotProps">
                {{ slotProps.value.display }}
              </template>
              <template #option="slotProps">
                {{ slotProps.option.display }}
              </template>
            </MultiSelect>
            <label for="ncos">Net Control Operators</label>
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
import MultiSelect from 'primevue/multiselect';
import API from '@/services/API';
import { useVuelidate } from '@vuelidate/core';
import { required, numeric, maxLength } from '@vuelidate/validators';

export default {
  components: {
    Card,
    PVButton: Button,
    InputText,
    MultiSelect,
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
      admins: [],
      ncos: [],
      allUsers: [],
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
            this.allUsers[
              i
            ].display = `${this.allUsers[i].id} - ${this.allUsers[i].callsign}`;
            // Remove user with id 9990 (parrot)
            if (this.allUsers[i].id === 9990) {
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
    handleTalkgroup(isFormValid) {
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
        .then((_res) => {
          API.post(`/talkgroups/${numericID}/admins`, {
            user_ids: this.admins.map((admin) => admin.id),
          })
            .then(() => {
              API.post(`/talkgroups/${numericID}/ncos`, {
                user_ids: this.ncos.map((nco) => nco.id),
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

<style scoped></style>
