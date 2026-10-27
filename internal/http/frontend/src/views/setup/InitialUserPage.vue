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
    <div
      role="alert"
      class="my-4 flex flex-col gap-2 rounded-lg border border-amber-500/55 bg-amber-500/10 px-4 py-3 text-sm"
      data-test="first-admin-banner"
    >
      <b class="font-semibold">No admin exists yet. Whoever creates this account first becomes the admin.</b>
      <p>
        The admin has full control of this DMRHub. Anyone who can open this page can create the account, so finish
        setup before you expose DMRHub to the internet.
      </p>
    </div>
    <UserRegistrationCard
      title="Create the first admin"
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
    title: 'Create the first admin',
  },
  methods: {
    complete() {
      API.post('/setupwizard/complete', {})
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
            detail: err.response?.data?.error ?? 'Failed to complete setup wizard',
            life: 3000,
          });
        });
    },
    handleRegister(data: Record<string, unknown>) {
      API.post('/users', data)
        .then((res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: res.data.message,
            life: 3000,
          });
          this.complete();
        })
        .catch((err) => {
          console.error(err);
          this.$toast.add({
            severity: 'error',
            summary: 'Error',
            detail: err.response?.data?.error ?? 'An unknown error occurred',
            life: 3000,
          });
          if (err.response?.status === 409) {
            this.complete();
          }
        });
    },
  },
};
</script>

<style scoped></style>
