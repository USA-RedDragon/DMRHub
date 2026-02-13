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
    <Card>
      <CardHeader>
        <CardTitle>DMR Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <p>These settings configure DMR related features in DMRHub.</p>
        <br />
        <div class="checkbox-row">
          <input
            id="disable-radio-id-validation"
            type="checkbox"
            v-model="disableRadioIDValidation"
          />
          <label for="disable-radio-id-validation">Disable Radio ID validation</label>
        </div>
        <p>
          When enabled, DMRHub allows any 7- to 9-digit radio ID without checking the Radio ID database.
        </p>
        <span v-if="errors && errors['disable-radio-id-validation']" class="p-error">
          {{ errors['disable-radio-id-validation'] }}
        </span>
        <br />
        <label class="field-label" for="radio-id-url">Radio ID URL</label>
        <ShadInput
          id="radio-id-url"
          type="text"
          v-model="radioIDURL"
          :aria-invalid="(errors && errors['radio-id-url']) || false"
        />
        <p>
          URL to fetch radio ID information for validation and display purposes. Expected JSON format is the same
          as RadioID.net.
        </p>
        <span v-if="errors && errors['radio-id-url']" class="p-error">{{ errors['radio-id-url'] }}</span>
        <br />
        <label class="field-label" for="repeater-id-url">Repeater ID URL</label>
        <ShadInput
          id="repeater-id-url"
          type="text"
          v-model="repeaterIDURL"
          :aria-invalid="(errors && errors['repeater-id-url']) || false"
        />
        <p>
          URL to fetch repeater information for validation and display purposes. Expected JSON format is the same
          as RadioID.net.
        </p>
        <span v-if="errors && errors['repeater-id-url']" class="p-error">{{ errors['repeater-id-url'] }}</span>
        <br />
        <MMDVMSettings v-model="mmdvm" :errors="errors.mmdvm" />
        <IPSCSettings v-model="ipsc" :errors="errors.ipsc" />
        <br v-if="false" />
        <OpenBridgeSettings v-model="openbridge" :errors="errors.openbridge" v-if="false"/>
      </CardContent>
    </Card>
  </div>
</template>

<script lang="ts">
import MMDVMSettings from './DMR/MMDVMSettings.vue';
import OpenBridgeSettings from './DMR/OpenBridgeSettings.vue';
import IPSCSettings from './DMR/IPSCSettings.vue';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input as ShadInput } from '@/components/ui/input';

export default {
  components: {
    MMDVMSettings,
    OpenBridgeSettings,
    IPSCSettings,
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
      required: true,
      default: () => ({}),
    },
  },
  emits: ['update:modelValue'],
  computed: {
    mmdvm: {
      get() {
        return (this.modelValue && this.modelValue['mmdvm']) || {};
      },
      set(value: Record<string, unknown>) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'mmdvm': value,
        });
      },
    },
    openbridge: {
      get() {
        return (this.modelValue && this.modelValue['openbridge']) || {};
      },
      set(value: Record<string, unknown>) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'openbridge': value,
        });
      },
    },
    ipsc: {
      get() {
        return (this.modelValue && this.modelValue['ipsc']) || {};
      },
      set(value: Record<string, unknown>) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'ipsc': value,
        });
      },
    },
    disableRadioIDValidation: {
      get() {
        return (this.modelValue && this.modelValue['disable-radio-id-validation']) || false;
      },
      set(value: boolean) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'disable-radio-id-validation': value,
        });
      },
    },
    radioIDURL: {
      get() {
        return (this.modelValue && this.modelValue['radio-id-url']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'radio-id-url': value,
        });
      },
    },
    repeaterIDURL: {
      get() {
        return (this.modelValue && this.modelValue['repeater-id-url']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'repeater-id-url': value,
        });
      },
    },
    robotsTXT: {
      get() {
        return (this.modelValue && this.modelValue['robots-txt']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'robots-txt': value,
        });
      },
    },
    cors: {
      get() {
        return (this.modelValue && this.modelValue['cors']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'cors': value,
        });
      },
    },
    trustedProxies: {
      get() {
        return (this.modelValue && this.modelValue['trusted-proxies']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'trusted-proxies': value,
        });
      },
    },
    canonicalHost: {
      get() {
        return (this.modelValue && this.modelValue['canonical-host']) || '';
      },
      set(value: string) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'canonical-host': value,
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
