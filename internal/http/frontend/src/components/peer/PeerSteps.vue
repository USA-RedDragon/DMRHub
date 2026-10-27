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
    <template v-if="tab === 'freedmr'">
      <TestedPill tested text="FreeDMR 546165e" />
      <h3>1. Add the link to freedmr.cfg</h3>
      <CodeBlock title="freedmr.cfg" :segments="freeDMRCfg" />
      <h3>2. Restart FreeDMR</h3>
      <CodeBlock title="Shell" :segments="freeDMRShell" />
    </template>

    <template v-else-if="tab === 'hblink3'">
      <TestedPill tested text="HBlink3 1db2730" />
      <h3>1. Add the link to hblink.cfg</h3>
      <CodeBlock title="hblink.cfg" :segments="hblinkCfg" />
      <h3>2. Bridge it in rules.py</h3>
      <p>This bridges TG 3100 to timeslot 1 of your <code>MASTER</code> system, here <code>LOCAL</code>.</p>
      <CodeBlock title="rules.py" :segments="hblinkRules" />
      <h3>3. Restart HBlink3</h3>
      <CodeBlock title="Shell" :segments="hblinkShell" />
    </template>

    <template v-else-if="tab === 'other'">
      <StepTable
        :head="['Setting', 'Value']"
        :rows="[
          ['Protocol', 'OpenBridge version 1'],
          ['Both slots', 'On'],
          ['Network ID', val(values.id)],
          ['Passphrase', val(values.password)],
          ['Target address', val(values.host)],
          ['Target port', val(values.port)],
          ['Listen address', val(values.ip)],
          ['Listen port', val(values.peerPort)],
        ]"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { snippet, val } from '@/lib/enroll';
import type { PeerTab, PeerValues } from '@/lib/peer';
import CodeBlock from '@/components/enroll/CodeBlock.vue';
import StepTable from '@/components/enroll/StepTable.vue';
import TestedPill from '@/components/enroll/TestedPill.vue';

const props = defineProps<{ tab: PeerTab; values: PeerValues }>();

const freeDMRCfg = computed(() => {
  const v = props.values;
  return snippet`[GLOBAL]
SERVER_ID: ${v.id}
VALIDATE_SERVER_IDS: False

[OBP-DMRHUB]
MODE: OPENBRIDGE
ENABLED: True
IP: ${v.ip}
PORT: ${v.peerPort}
NETWORK_ID: ${v.id}
PASSPHRASE: ${v.password}
TARGET_IP: ${v.host}
TARGET_PORT: ${v.port}
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_ACL: PERMIT:ALL
RELAX_CHECKS: True
ENHANCED_OBP: True
PROTO_VER: 5`;
});

const freeDMRShell = computed(() => snippet`python bridge_master.py -c freedmr.cfg`);

const hblinkCfg = computed(() => {
  const v = props.values;
  return snippet`[OBP-DMRHUB]
MODE: OPENBRIDGE
ENABLED: True
IP: ${v.ip}
PORT: ${v.peerPort}
NETWORK_ID: ${v.id}
PASSPHRASE: ${v.password}
TARGET_IP: ${v.host}
TARGET_PORT: ${v.port}
BOTH_SLOTS: True
USE_ACL: False
SUB_ACL: PERMIT:ALL
TGID_ACL: PERMIT:ALL`;
});

const hblinkRules = computed(() => snippet`BRIDGES = {
    'TG3100': [
        {'SYSTEM': 'OBP-DMRHUB', 'TS': 1, 'TGID': 3100, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
        {'SYSTEM': '${'LOCAL'}',      'TS': 1, 'TGID': 3100, 'ACTIVE': True, 'TIMEOUT': 2, 'TO_TYPE': 'NONE', 'ON': [], 'OFF': [], 'RESET': []},
    ],
}
UNIT = ['OBP-DMRHUB', '${'LOCAL'}']`);

const hblinkShell = computed(() => snippet`python bridge.py -c hblink.cfg -r rules.py`);

</script>
