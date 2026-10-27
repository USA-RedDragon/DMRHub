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

import { defineComponent } from 'vue';
import SettingBadges from '@/components/setup/SettingBadges.vue';
import SettingNote from '@/components/setup/SettingNote.vue';
import { setupStateKey, settingLocked, type SetupState } from '@/lib/setupState';
import { settingDirective } from '@/lib/settingDirective';

/**
 * settingMixin gives a Setup card the state of its settings: the
 * SettingBadges and SettingNote components, the v-setting directive and
 * settingLocked.
 */
export const settingMixin = defineComponent({
  components: { SettingBadges, SettingNote },
  directives: { setting: settingDirective },
  inject: {
    setupState: { from: setupStateKey as symbol, default: null },
  },
  methods: {
    settingLocked(path: string): boolean {
      return settingLocked(this.setupState as SetupState | null, path);
    },
  },
});
