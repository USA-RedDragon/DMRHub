// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

import { computed, onMounted, onUnmounted, ref } from 'vue';

const COLOR_MODE_KEY = 'color-mode';
type ColorMode = 'light' | 'dark' | 'system';

const storedMode = localStorage.getItem(COLOR_MODE_KEY);
const mode = ref<ColorMode>(
  storedMode === 'light' || storedMode === 'dark' || storedMode === 'system'
    ? storedMode
    : 'system',
);
const isDark = ref(false);

let mediaQuery: MediaQueryList | null = null;

const resolveIsDark = () => {
  if (mode.value === 'dark') {
    return true;
  }
  if (mode.value === 'light') {
    return false;
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
};

const applyMode = () => {
  isDark.value = resolveIsDark();
  if (isDark.value) {
    document.documentElement.classList.add('dark');
  } else {
    document.documentElement.classList.remove('dark');
  }
};

const onSystemThemeChange = () => {
  if (mode.value === 'system') {
    applyMode();
  }
};

const setMode = (newMode: ColorMode) => {
  mode.value = newMode;
  localStorage.setItem(COLOR_MODE_KEY, newMode);
  applyMode();
};

const cycleMode = () => {
  if (mode.value === 'system') {
    setMode('light');
    return;
  }
  if (mode.value === 'light') {
    setMode('dark');
    return;
  }
  setMode('system');
};

const setup = () => {
  mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
  mediaQuery.addEventListener('change', onSystemThemeChange);
  applyMode();
};

const cleanup = () => {
  if (!mediaQuery) {
    return;
  }
  mediaQuery.removeEventListener('change', onSystemThemeChange);
};

export function useColorMode() {
  onMounted(setup);
  onUnmounted(cleanup);

  return {
    mode,
    isDark,
    modeLabel: computed(() => {
      if (mode.value === 'system') {
        return 'System';
      }
      if (mode.value === 'dark') {
        return 'Dark';
      }
      return 'Light';
    }),
    setMode,
    cycleMode,
  };
}
