// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

import API from './API.js';

let features = [];

export default {
  OpenBridge: 'openbridge',
  getFeatures() {
    return new Promise((resolve, reject) => {
      API.get('/features').then((response) => {
        if (typeof response.data !== 'object' || !('features' in response.data)) {
          return;
        }
        features = response.data.features;
        resolve(this);
      }).catch((error) => {
        console.error(error);
        reject(error);
      });
    });
  },
  isEnabled(feature) {
    return features.includes(feature);
  },
};
