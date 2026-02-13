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
    <UserRegistrationCard
      title="Create Initial Admin User"
      @register="handleRegister"
    />
  </div>
</template>

<script lang="ts">
import UserRegistrationCard from '@/components/UserRegistrationCard.vue';
import API from '@/services/API';

export default {
  components: {
    UserRegistrationCard,
  },
  head: {
    title: 'Initial User Setup',
  },
  created() {},
  mounted() {},
  data: function() {
    return {};
  },
  methods: {
    handleRegister(data: Record<string, unknown>) {
      API.post('/users', data, { headers: { 'X-SetupWizard-Token': String(this.$route.query.token ?? '') } })
        .then((res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: res.data.message,
            life: 3000,
          });
          API.post('/setupwizard/complete', {}, { headers: { 'X-SetupWizard-Token': String(this.$route.query.token ?? '') } })
            .then(() => {
              this.$toast.add({
                severity: 'success',
                summary: 'Success',
                detail: 'Setup wizard completed successfully',
                life: 3000,
              });
              setTimeout(() => {
                window.location.href = '/login';
              }, 1000);
            })
            .catch((err) => {
              console.error(err);
              this.$toast.add({
                severity: 'error',
                summary: 'Error',
                detail: 'Failed to complete setup wizard',
                life: 3000,
              });
            });
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
