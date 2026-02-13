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

import axios from 'axios';

const baseURL = '/api/v1';

const instance = axios.create({
  baseURL,
  withCredentials: true,
});

instance.interceptors.response.use(
  (response) => {
    return response;
  },
  (error) => {
    if (error.response === undefined) {
      return Promise.reject(error);
    }

    const status = error.response.status;
    if (
      window.location.pathname !== '/login' &&
      window.location.pathname !== '/' &&
      window.location.pathname !== '/lastheard' &&
      window.location.pathname !== '/register' &&
      (status === 401 || status === 403)
    ) {
      window.location.pathname = '/login';
    }

    return Promise.reject(error);
  },
);

export default instance;
