<!--
  SPDX-License-Identifier: AGPL-3.0-or-later
  DMRHub - Run a DMR network server in a single binary
  Copyright (C) 2023 Jacob McSwain

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

<script setup>
import Button from 'primevue/button';
import Sidebar from 'primevue/sidebar';

import { ref } from 'vue';
import { useLayout } from '@/layout/composables/layout';

defineProps({
  simple: {
    type: Boolean,
    default: false,
  },
});
const scales = ref([10, 12, 14, 16, 18, 20, 22]);
const visible = ref(false);

const { changeThemeSettings, setScale, layoutConfig } = useLayout();

const onConfigButtonClick = () => {
  visible.value = !visible.value;
};
const onChangeTheme = (theme) => {
  // Save layoutConfig to localStorage
  window.localStorage.setItem(
    'theme',
    JSON.stringify(theme).replace('"', '').replace('"', ''),
  );
  const elementId = 'theme-css';
  const linkElement = document.getElementById(elementId);
  const cloneLinkElement = linkElement.cloneNode(true);
  const newThemeUrl = linkElement
    .getAttribute('href')
    .replace(layoutConfig.theme.value, theme);
  cloneLinkElement.setAttribute('id', elementId + '-clone');
  cloneLinkElement.setAttribute('href', newThemeUrl);
  cloneLinkElement.addEventListener('load', () => {
    linkElement.remove();
    cloneLinkElement.setAttribute('id', elementId);
    changeThemeSettings(theme);
  });
  linkElement.parentNode.insertBefore(
    cloneLinkElement,
    linkElement.nextSibling,
  );
};

const theme = window.localStorage.getItem('theme');
if (theme) {
  onChangeTheme(theme);
}

const decrementScale = () => {
  setScale(layoutConfig.scale.value - 2);
  applyScale();
};
const incrementScale = () => {
  setScale(layoutConfig.scale.value + 2);
  applyScale();
};
const applyScale = () => {
  document.documentElement.style.fontSize = layoutConfig.scale.value + 'px';
};

const scale = window.localStorage.getItem('scale');
if (scale) {
  setScale(parseInt(scale));
  applyScale();
}
</script>

<template>
  <button
    class="layout-config-button p-link"
    type="button"
    @click="onConfigButtonClick()"
  >
    <i class="pi pi-palette"></i>
  </button>

  <Sidebar
    v-model:visible="visible"
    position="right"
    :transitionOptions="'.3s cubic-bezier(0, 0, 0.2, 1)'"
    class="layout-config-sidebar w-20rem"
  >
    <h5>Scale</h5>
    <div class="flex align-items-center">
      <Button
        icon="pi pi-minus"
        type="button"
        @click="decrementScale()"
        class="p-button-text p-button-rounded w-2rem h-2rem mr-2"
        :disabled="layoutConfig.scale.value === scales[0]"
      ></Button>
      <div class="flex gap-2 align-items-center">
        <i
          class="pi pi-circle-fill text-300"
          v-for="s in scales"
          :key="s"
          :class="{ 'text-primary-500': s === layoutConfig.scale.value }"
        ></i>
      </div>
      <Button
        icon="pi pi-plus"
        type="button"
        pButton
        @click="incrementScale()"
        class="p-button-text p-button-rounded w-2rem h-2rem ml-2"
        :disabled="layoutConfig.scale.value === scales[scales.length - 1]"
      ></Button>
    </div>

    <h5>Bootstrap</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('bootstrap4-light-blue', 'light')"
        >
          <img
            src="/layout/images/themes/bootstrap4-light-blue.svg"
            class="w-2rem h-2rem"
            alt="Bootstrap Light Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('bootstrap4-light-purple', 'light')"
        >
          <img
            src="/layout/images/themes/bootstrap4-light-purple.svg"
            class="w-2rem h-2rem"
            alt="Bootstrap Light Purple"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('bootstrap4-dark-blue', 'dark')"
        >
          <img
            src="/layout/images/themes/bootstrap4-dark-blue.svg"
            class="w-2rem h-2rem"
            alt="Bootstrap Dark Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('bootstrap4-dark-purple', 'dark')"
        >
          <img
            src="/layout/images/themes/bootstrap4-dark-purple.svg"
            class="w-2rem h-2rem"
            alt="Bootstrap Dark Purple"
          />
        </button>
      </div>
    </div>

    <h5>Material Design</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('md-light-indigo', 'light')"
        >
          <img
            src="/layout/images/themes/md-light-indigo.svg"
            class="w-2rem h-2rem"
            alt="Material Light Indigo"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('md-light-deeppurple', 'light')"
        >
          <img
            src="/layout/images/themes/md-light-deeppurple.svg"
            class="w-2rem h-2rem"
            alt="Material Light DeepPurple"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('md-dark-indigo', 'dark')"
        >
          <img
            src="/layout/images/themes/md-dark-indigo.svg"
            class="w-2rem h-2rem"
            alt="Material Dark Indigo"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('md-dark-deeppurple', 'dark')"
        >
          <img
            src="/layout/images/themes/md-dark-deeppurple.svg"
            class="w-2rem h-2rem"
            alt="Material Dark DeepPurple"
          />
        </button>
      </div>
    </div>

    <h5>Material Design Compact</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('mdc-light-indigo', 'light')"
        >
          <img
            src="/layout/images/themes/md-light-indigo.svg"
            class="w-2rem h-2rem"
            alt="Material Light Indigo"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('mdc-light-deeppurple', 'light')"
        >
          <img
            src="/layout/images/themes/md-light-deeppurple.svg"
            class="w-2rem h-2rem"
            alt="Material Light Deep Purple"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('mdc-dark-indigo', 'dark')"
        >
          <img
            src="/layout/images/themes/md-dark-indigo.svg"
            class="w-2rem h-2rem"
            alt="Material Dark Indigo"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('mdc-dark-deeppurple', 'dark')"
        >
          <img
            src="/layout/images/themes/md-dark-deeppurple.svg"
            class="w-2rem h-2rem"
            alt="Material Dark Deep Purple"
          />
        </button>
      </div>
    </div>

    <h5>Tailwind</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('tailwind-light', 'light')"
        >
          <img
            src="/layout/images/themes/tailwind-light.png"
            class="w-2rem h-2rem"
            alt="Tailwind Light"
          />
        </button>
      </div>
    </div>

    <h5>Fluent UI</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('fluent-light', 'light')"
        >
          <img
            src="/layout/images/themes/fluent-light.png"
            class="w-2rem h-2rem"
            alt="Fluent Light"
          />
        </button>
      </div>
    </div>

    <h5>Lara</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-light-indigo', 'light')"
        >
          <img
            src="/layout/images/themes/lara-light-indigo.png"
            class="w-2rem h-2rem"
            alt="Lara Light Indigo"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-light-blue', 'light')"
        >
          <img
            src="/layout/images/themes/lara-light-blue.png"
            class="w-2rem h-2rem"
            alt="Lara Light Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-light-purple', 'light')"
        >
          <img
            src="/layout/images/themes/lara-light-purple.png"
            class="w-2rem h-2rem"
            alt="Lara Light Purple"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-light-teal', 'light')"
        >
          <img
            src="/layout/images/themes/lara-light-teal.png"
            class="w-2rem h-2rem"
            alt="Lara Light Teal"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-dark-indigo', 'dark')"
        >
          <img
            src="/layout/images/themes/lara-dark-indigo.png"
            class="w-2rem h-2rem"
            alt="Lara Dark Indigo"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-dark-blue', 'dark')"
        >
          <img
            src="/layout/images/themes/lara-dark-blue.png"
            class="w-2rem h-2rem"
            alt="Lara Dark Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-dark-purple', 'dark')"
        >
          <img
            src="/layout/images/themes/lara-dark-purple.png"
            class="w-2rem h-2rem"
            alt="Lara Dark Purple"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('lara-dark-teal', 'dark')"
        >
          <img
            src="/layout/images/themes/lara-dark-teal.png"
            class="w-2rem h-2rem"
            alt="Lara Dark Teal"
          />
        </button>
      </div>
    </div>

    <h5>Luna</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('luna-amber', 'dark')"
        >
          <img
            src="/layout/images/themes/luna-amber.png"
            class="w-2rem h-2rem"
            alt="Luna Amber"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('luna-blue', 'dark')"
        >
          <img
            src="/layout/images/themes/luna-blue.png"
            class="w-2rem h-2rem"
            alt="Luna Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('luna-green', 'dark')"
        >
          <img
            src="/layout/images/themes/luna-green.png"
            class="w-2rem h-2rem"
            alt="Luna Green"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('luna-pink', 'dark')"
        >
          <img
            src="/layout/images/themes/luna-pink.png"
            class="w-2rem h-2rem"
            alt="Luna Pink"
          />
        </button>
      </div>
    </div>

    <h5>Saga</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('saga-blue', 'light')"
        >
          <img
            src="/layout/images/themes/saga-blue.png"
            class="w-2rem h-2rem"
            alt="Saga Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('saga-green', 'light')"
        >
          <img
            src="/layout/images/themes/saga-green.png"
            class="w-2rem h-2rem"
            alt="Saga Green"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('saga-orange', 'light')"
        >
          <img
            src="/layout/images/themes/saga-orange.png"
            class="w-2rem h-2rem"
            alt="Saga Orange"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('saga-purple', 'light')"
        >
          <img
            src="/layout/images/themes/saga-purple.png"
            class="w-2rem h-2rem"
            alt="Saga Purple"
          />
        </button>
      </div>
    </div>
    <h5>Vela</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('vela-blue', 'dark')"
        >
          <img
            src="/layout/images/themes/vela-blue.png"
            class="w-2rem h-2rem"
            alt="Vela Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('vela-green', 'dark')"
        >
          <img
            src="/layout/images/themes/vela-green.png"
            class="w-2rem h-2rem"
            alt="Vela Green"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('vela-orange', 'dark')"
        >
          <img
            src="/layout/images/themes/vela-orange.png"
            class="w-2rem h-2rem"
            alt="Vela Orange"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('vela-purple', 'dark')"
        >
          <img
            src="/layout/images/themes/vela-purple.png"
            class="w-2rem h-2rem"
            alt="Vela Purple"
          />
        </button>
      </div>
    </div>
    <h5>Arya</h5>
    <div class="grid">
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('arya-blue', 'dark')"
        >
          <img
            src="/layout/images/themes/arya-blue.png"
            class="w-2rem h-2rem"
            alt="Arya Blue"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('arya-green', 'dark')"
        >
          <img
            src="/layout/images/themes/arya-green.png"
            class="w-2rem h-2rem"
            alt="Arya Green"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('arya-orange', 'dark')"
        >
          <img
            src="/layout/images/themes/arya-orange.png"
            class="w-2rem h-2rem"
            alt="Arya Orange"
          />
        </button>
      </div>
      <div class="col-3">
        <button
          class="p-link w-2rem h-2rem"
          @click="onChangeTheme('arya-purple', 'dark')"
        >
          <img
            src="/layout/images/themes/arya-purple.png"
            class="w-2rem h-2rem"
            alt="Arya Purple"
          />
        </button>
      </div>
    </div>
  </Sidebar>
</template>

<style lang="scss" scoped>
.layout-config-button {
  display: block;
  position: fixed;
  width: 3rem;
  height: 3rem;
  line-height: 3rem;
  background: var(--primary-color);
  color: var(--primary-color-text);
  text-align: center;
  top: 50%;
  right: 0;
  margin-top: -1.5rem;
  border-top-left-radius: var(--border-radius);
  border-bottom-left-radius: var(--border-radius);
  border-top-right-radius: 0;
  border-bottom-right-radius: 0;
  transition: background-color var(--transition-duration);
  overflow: hidden;
  cursor: pointer;
  z-index: 999;
  box-shadow: -0.25rem 0 1rem rgba(0, 0, 0, 0.15);

  i {
    font-size: 2rem;
    line-height: inherit;
    transform: rotate(0deg);
    transition: transform 1s;
  }

  &:hover {
    background: var(--primary-400);
  }
}

.layout-config-sidebar {
  &.p-sidebar {
    .p-sidebar-content {
      padding-left: 2rem;
      padding-right: 2rem;
    }
  }
}
</style>
