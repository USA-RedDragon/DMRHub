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
  <div class="enroll-panel">
    <TestedPill tested text="DroidStar becf1de (Android)" />
    <h3>1. Add DMRHub on the Hosts tab</h3>
    <p>Open the <b>Hosts</b> tab and type this line in the text box:</p>
    <CodeBlock title="Hosts tab" :segments="hostLine" />
    <h3>2. Set the DMR fields</h3>
    <StepTable
      :head="['Tab', 'Field', 'Value']"
      :rows="[
        ['Settings', 'Callsign', val(values.callsign)],
        ['Settings', 'DMRID', val(values.ownerId)],
        ['Settings', 'ESSID', val(values.essid)],
        ['Settings', 'DMR+ Opts', val('TS1=91;TS2=3100;')],
        ['Main', 'Mode', 'DMR'],
        ['Main', 'Host', val('DMRHub')],
        ['Main', 'TGID', val('3100')],
      ]"
    />
    <h3>3. Connect</h3>
    <p>
      On the <b>Main</b> tab, tap <b>Connect</b>. To test, set <b>TGID</b> to <code>9990</code>, tap <b>TX</b>, talk,
      and tap <b>TX</b> again. The parrot plays your call back.
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { snippet, val, type ConnectValues } from '@/lib/enroll';
import CodeBlock from './CodeBlock.vue';
import StepTable from './StepTable.vue';
import TestedPill from './TestedPill.vue';

const props = defineProps<{ values: ConnectValues }>();

const hostLine = computed(() => {
  const v = props.values;
  return snippet`DMR ${'DMRHub'} ${v.host} ${v.port} ${v.password}`;
});
</script>
