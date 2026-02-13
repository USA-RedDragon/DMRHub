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
  <form @submit.prevent="handleRegister(!v$.$invalid)">
    <Card>
      <CardHeader>
        <CardTitle>{{ title }}</CardTitle>
      </CardHeader>
      <CardContent>
        <label class="field-label" for="dmr_id">DMR ID</label>
        <ShadInput
          id="dmr_id"
          type="text"
          v-model="v$.dmr_id.$model"
          :aria-invalid="v$.dmr_id.$invalid && submitted"
        />
        <span v-if="v$.dmr_id.$error && submitted">
          <span v-for="(error, index) of v$.dmr_id.$errors" :key="index">
            <small class="field-error">{{ error.$message.replace("Value", "DMR ID") }}</small>
            <br />
          </span>
        </span>
        <span v-else>
          <small
            v-if="
              (v$.dmr_id.$invalid && submitted) ||
              v$.dmr_id.$pending.$response
            "
            class="field-error"
            >{{ v$.dmr_id.required.$message.replace("Value", "DMR ID") }}
            <br />
          </small>
        </span>
        <br />
        <label class="field-label" for="username">Username</label>
        <ShadInput
          id="username"
          type="text"
          v-model="v$.username.$model"
          :aria-invalid="v$.username.$invalid && submitted"
        />
        <span v-if="v$.username.$error && submitted">
          <span v-for="(error, index) of v$.username.$errors" :key="index">
            <small class="field-error">{{ error.$message.replace("Value", "Username") }}</small>
            <br />
          </span>
        </span>
        <span v-else>
          <small
            v-if="
              (v$.username.$invalid && submitted) ||
              v$.username.$pending.$response
            "
            class="field-error"
            >{{ v$.username.required.$message.replace("Value", "Username") }}
            <br />
          </small>
        </span>
        <br />
        <label class="field-label" for="callsign">Callsign</label>
        <ShadInput
          id="callsign"
          type="text"
          v-model="v$.callsign.$model"
          :aria-invalid="v$.callsign.$invalid && submitted"
        />
        <span v-if="v$.callsign.$error && submitted">
          <span v-for="(error, index) of v$.callsign.$errors" :key="index">
            <small class="field-error">{{ error.$message.replace("Value", "Callsign") }}</small>
            <br />
          </span>
        </span>
        <span v-else>
          <small
            v-if="
              (v$.callsign.$invalid && submitted) ||
              v$.callsign.$pending.$response
            "
            class="field-error"
            >{{ v$.callsign.required.$message.replace("Value", "Callsign") }}
            <br />
          </small>
        </span>
        <br />
        <label class="field-label" for="password">Password</label>
        <ShadInput
          id="password"
          type="password"
          v-model="v$.password.$model"
          :aria-invalid="v$.password.$invalid && submitted"
        />
        <span v-if="v$.password.$error && submitted">
          <span v-for="(error, index) of v$.password.$errors" :key="index">
            <small class="field-error">{{ error.$message.replace("Value", "Password") }}</small>
            <br />
          </span>
        </span>
        <span v-else>
          <small
            v-if="
              (v$.password.$invalid && submitted) ||
              v$.password.$pending.$response
            "
            class="field-error"
            >{{ v$.password.required.$message.replace("Value", "Password") }}
            <br />
          </small>
        </span>
        <br />
        <label class="field-label" for="confirmPassword">Confirm Password</label>
        <ShadInput
          id="confirmPassword"
          type="password"
          v-model="v$.confirmPassword.$model"
          :aria-invalid="v$.confirmPassword.$invalid && submitted"
        />
        <span v-if="v$.confirmPassword.$error && submitted">
          <span
            v-for="(error, index) of v$.confirmPassword.$errors"
            :key="index"
          >
            <small class="field-error">{{ error.$message.replace("Value", "Confirm Password") }}</small>
            <br />
          </span>
        </span>
        <span v-else>
          <small
            v-if="
              (v$.confirmPassword.$invalid && submitted) ||
              v$.confirmPassword.$pending.$response
            "
            class="field-error"
            >{{ v$.confirmPassword.required.$message.replace("Value", "Confirm Password") }}
            <br />
          </small>
        </span>
      </CardContent>
      <CardFooter>
        <div class="card-footer">
          <ShadButton type="submit" variant="outline" size="sm">Create User</ShadButton>
        </div>
      </CardFooter>
    </Card>
  </form>
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

import { useVuelidate } from '@vuelidate/core';
import { required, sameAs, numeric } from '@vuelidate/validators';

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
  setup: () => ({ v$: useVuelidate() }),
  props: {
    'title': {
      type: String,
      default: 'Register User',
    },
  },
  emits: ['register'],
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
    handleRegister(isFormValid: boolean) {
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
      this.$emit('register', {
        id: numericID,
        callsign: this.callsign.trim(),
        username: this.username.trim(),
        password: this.password.trim(),
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

.field-error {
  color: hsl(var(--primary));
}

</style>
