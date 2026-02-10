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
      <template #title>DMR Settings</template>
      <template #content>
        <p>These settings configure DMR related features in DMRHub.</p>
        <br />
        <MMDVMSettings v-model="mmdvm" :errors="errors.mmdvm" />
        <br v-if="false" />
        <OpenBridgeSettings v-model="openbridge" :errors="errors.openbridge" v-if="false"/>
      </template>
    </Card>
  </div>
</template>

<script>
import MMDVMSettings from './DMR/MMDVMSettings.vue';
import OpenBridgeSettings from './DMR/OpenBridgeSettings.vue';

import Card from 'primevue/card';

export default {
  components: {
    MMDVMSettings,
    OpenBridgeSettings,
    Card,
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
      set(value) {
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
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'openbridge': value,
        });
      },
    },
    robotsTXT: {
      get() {
        return (this.modelValue && this.modelValue['robots-txt']) || '';
      },
      set(value) {
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
      set(value) {
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
      set(value) {
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
      set(value) {
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
