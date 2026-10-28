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
        <h1>Welcome to DMRHub</h1>
      </CardHeader>
      <CardContent>
        <h2>What is DMRHub?</h2>
        <p>
          DMRHub is an <a href="https://github.com/USA-RedDragon/DMRHub" target="_blank">open-source</a> DMR
          network server for hotspots and repeaters that use the MMDVM network protocol. The idea is to be an open source, easy to use alternative to existing
          networks like BrandMeister and TGIF, but for use in local communication environments.
        </p>
        <br />
        <h2>Special Talkgroups</h2>
        <p>
          Talkgroup 4000 unlinks the dynamic talkgroup of the timeslot you call it on. The parrot is a plugin.
          When the admin installed the parrot plugin, it answers on 9990.
        </p>
        <br />
        <h2>Useful Links</h2>
        <ul>
          <li>
            <a href="https://github.com/USA-RedDragon/DMRHub/blob/main/doc/User-Guide.md" target="_blank">DMRHub User's Guide</a>
          </li>
          <li>
            <a href="https://github.com/USA-RedDragon/DMRHub/blob/main/doc/admin/overview.md" target="_blank">
              DMRHub Admin's Guide</a>
          </li>
          <li>
            <a href="https://radio.dmrhub.net" target="_blank">DMRHub Browser Radio</a>, a DMR radio in your web browser
          </li>
        </ul>
      </CardContent>
    </Card>
  </div>
</template>

<script lang="ts">
import { Card, CardContent, CardHeader } from '@/components/ui/card';

export default {
  components: {
    Card,
    CardContent,
    CardHeader,
  },
  head: {
    title: 'Home',
  },
  created() {},
  mounted() {},
  unmounted() {},
  data: function() {
    return {};
  },
  methods: {},
  computed: {},
};
</script>

<style scoped>
ul {
  list-style: none;
  padding-left: 0;
}

li {
  margin-bottom: 0.25rem;
}
</style>
