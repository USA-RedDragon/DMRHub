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
    <p v-if="settingsStore.registration === 'closed'" class="text-center text-muted-foreground" data-test="register-closed">
      Registration is closed.
    </p>
    <p
      v-else-if="settingsStore.registration === 'invite' && !invite"
      class="text-center text-muted-foreground"
      data-test="register-invite-only"
    >
      Registration needs an invite.
    </p>
    <UserRegistrationCard
      v-else
      :email-mode="settingsStore.passwordResetEnabled ? 'ask' : 'off'"
      @register="handleRegister"
    />
  </div>
</template>

<script lang="ts">
import UserRegistrationCard from '@/components/UserRegistrationCard.vue';
import API from '@/services/API';
import { useSettingsStore } from '@/store';

export default {
  components: {
    UserRegistrationCard,
  },
  head: {
    title: 'Register',
  },
  created() {
    void this.settingsStore.fetchConfig();
  },
  mounted() {},
  data: function() {
    return {
      settingsStore: useSettingsStore(),
      invite: new URLSearchParams(window.location.hash.replace(/^#/, '')).get('invite') ?? '',
    };
  },
  methods: {
    handleRegister(data: Record<string, unknown>) {
      API.post('/users', this.invite ? { ...data, invite: this.invite } : data)
        .then((res) => {
          this.$toast.add({
            severity: 'success',
            summary: 'Success',
            detail: res.data.message,
            life: 3000,
          });
          setTimeout(() => {
            this.$router.push('/login');
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
