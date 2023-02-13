<!--
  SPDX-License-Identifier: AGPL-3.0-only
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

<template>
  <div class="footer">
    <p>
      This project proudly open
      <a href="https://github.com/USA-RedDragon/DMRHub" target="_blank"
        >source</a
      >
    </p>
    <p>Copyright &copy; 2023-{{ year }} Jacob McSwain</p>
    <p id="version">Version {{ version }}</p>
  </div>
</template>

<script>
import API from "@/services/API";
export default {
  name: "Footer",
  data: function () {
    return {
      year: new Date().getFullYear(),
      version: "",
    };
  },
  // Get version from API onMounted
  mounted() {
    API.get("/version")
      .then((response) => {
        this.version = response.data;
      })
      .catch((error) => {
        console.log(error);
      });
  },
};
</script>

<style scoped>
.footer {
  text-align: center;
  display: block;
  padding: 1em;
  padding-bottom: 0;
  margin-bottom: 0;
  font-size: 0.8em;
}

.footer p#version {
  font-size: 0.8em;
  margin-top: 0;
  color: #666;
}
</style>
