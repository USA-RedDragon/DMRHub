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
    <Card>
      <CardHeader>
        <CardTitle>IPSC Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>DMRHub can run an IPSC server to allow connections from Motorola DMR radios</p>
        <br />
        <div class="checkbox-row">
          <input
            id="ipsc-enabled" v-setting="settingLocked('dmr.ipsc.enabled')"
            type="checkbox"
            v-model="enabled"
          />
          <label for="ipsc-enabled">Enabled <SettingBadges path="dmr.ipsc.enabled" /></label>
        </div>
        <SettingNote path="dmr.ipsc.enabled" />
        <br v-if="enabled" />
        <label class="field-label" for="ipsc-bind" v-if="enabled">Bind Address <SettingBadges path="dmr.ipsc.bind" /></label>
        <ShadInput id="ipsc-bind" v-setting="settingLocked('dmr.ipsc.bind')" type="text" v-model="bind" v-if="enabled" :aria-invalid="(errors && errors.bind) || false" />
        <p v-if="enabled">
          The address to bind the IPSC server to
        </p>
        <SettingNote v-if="enabled" path="dmr.ipsc.bind" />
        <span v-if="errors && errors.bind" class="p-error">{{ errors.bind }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="ipsc-port" v-if="enabled">Port <SettingBadges path="dmr.ipsc.port" /></label>
        <ShadInput
          id="ipsc-port" v-setting="settingLocked('dmr.ipsc.port')"
          type="number"
          v-model="port"
          v-if="enabled"
          :aria-invalid="(errors && errors.port) || false"
        />
        <p v-if="enabled">
          The port number to bind the IPSC server to
        </p>
        <SettingNote v-if="enabled" path="dmr.ipsc.port" />
        <span v-if="enabled && errors && errors.port" class="p-error">{{ errors.port }}</span>
        <br v-if="enabled" />
        <label class="field-label" for="ipsc-network-id" v-if="enabled">Network ID <SettingBadges path="dmr.ipsc.network-id" /></label>
        <ShadInput
          id="ipsc-network-id" v-setting="settingLocked('dmr.ipsc.network-id')"
          type="number"
          v-model="networkId"
          v-if="enabled"
          :aria-invalid="(errors && errors['network-id']) || false"
        />
        <p v-if="enabled">
          DMR peer ID for this IPSC master server (e.g., your repeater's radio ID)
        </p>
        <SettingNote v-if="enabled" path="dmr.ipsc.network-id" />
        <span v-if="enabled && errors && errors['network-id']" class="p-error">{{ errors['network-id'] }}</span>
      </CardContent>
    </Card>
  </div>
</template>

<script lang="ts">
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';
import { settingMixin } from '@/lib/settingMixin';

export default {
  mixins: [settingMixin],
  components: {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    ShadInput,
  },
  props: {
    modelValue: {
      type: Object,
      required: true,
    },
    errors: {
      type: Object,
    },
  },
  emits: ['update:modelValue'],
  computed: {
    enabled: {
      get() {
        return (this.modelValue && this.modelValue['enabled']) || false;
      },
      set(value: boolean) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'enabled': value,
        });
      },
    },
    bind: {
      get() {
        return (this.modelValue && this.modelValue['bind']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'bind': value,
        });
      },
    },
    port: {
      get() {
        return (this.modelValue && this.modelValue['port']) || undefined;
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'port': Number(value) || 0,
        });
      },
    },
    networkId: {
      get() {
        return (this.modelValue && this.modelValue['network-id']) || undefined;
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'network-id': Number(value) || 0,
        });
      },
    },
  },
  data: function() {
    return {};
  },
  mounted() {},
};
</script>

<style scoped>
.field-label {
  display: block;
  margin-bottom: 0.25rem;
}

.checkbox-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
