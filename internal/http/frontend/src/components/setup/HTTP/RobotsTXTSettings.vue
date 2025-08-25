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
      <template #title>robots.txt Settings</template>
      <template #content>
        <p>robots.txt settings</p>
        <br />
        <span class="p-float-label">
          <Dropdown id="mode" v-model="mode" optionValue="value" optionLabel="label" :options="[
            { label: 'Disabled', value: 'disabled' },
            { label: 'Allow', value: 'allow' },
            { label: 'Custom', value: 'custom' },
          ]" :class="{ 'p-invalid': (errors && errors.mode) || false }" />
          <label for="mode">Mode</label>
        </span>
        <p>
          The mode to use for the robots.txt file.
          One of <code>disabled</code>, <code>allow</code>, or <code>custom</code>.
          If <code>disabled</code> is selected, a robots.txt file will be served that disallows all web crawlers.
          If <code>allow</code> is selected, a robots.txt file will be served that allows all web crawlers.
          If <code>custom</code> is selected, the content of the robots.txt file can be customized.
        </p>
        <span v-if="errors && errors.mode" class="p-error">{{ errors.mode }}</span>
        <br v-if="mode === 'custom'" />
        <span class="p-float-label" v-if="mode === 'custom'">
          <TextArea
            rows="5"
            id="content"
            v-model="content"
            :class="{ 'p-invalid': (errors && errors.content) || false }"
          />
          <label for="content">Content</label>
        </span>
        <p v-if="mode === 'custom'">
          The content of the robots.txt file when the mode is set to <code>custom</code>.
        </p>
        <span v-if="mode === 'custom' && errors && errors.content" class="p-error">{{ errors.content }}</span>
      </template>
    </Card>
  </div>
</template>

<script>
import Card from 'primevue/card';
import TextArea from 'primevue/textarea';
import Dropdown from 'primevue/dropdown';

export default {
  components: {
    Card,
    TextArea,
    Dropdown,
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
    mode: {
      get() {
        return (this.modelValue && this.modelValue['mode']) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'mode': value,
        });
      },
    },
    content: {
      get() {
        return (this.modelValue && this.modelValue['content']) || '';
      },
      set(value) {
        this.$emit('update:modelValue', {
          ...this.modelValue,
          'content': value,
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
