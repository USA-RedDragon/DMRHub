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

import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router';
import { routes, setupwizardRoutes } from './routes';
import { useUserStore } from '@/store';

const passwordPages = ['/settings', '/login', '/forgot-password', '/reset-password', '/verify-email'];

/**
 * Sends a user whose password an admin set to Settings, until they choose
 * their own password there.
 */
export function ownPasswordGuard(to: RouteLocationNormalized) {
  const user = useUserStore();
  if (user.loggedIn && user.mustChangePassword && !passwordPages.includes(to.path)) {
    return { path: '/settings' };
  }
  return true;
}

export default (setupwizard: boolean) => {
  if (setupwizard) {
    return createRouter({
      history: createWebHistory(import.meta.env.BASE_URL),
      routes: setupwizardRoutes(),
    });
  }

  const router = createRouter({
    history: createWebHistory(import.meta.env.BASE_URL),
    routes: routes(),
  });
  router.beforeEach(ownPasswordGuard);

  return router;
};
