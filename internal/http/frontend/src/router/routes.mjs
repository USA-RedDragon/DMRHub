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

export default [
  {
    path: '/',
    name: 'Main',
    component: () => import('../views/MainPage.vue'),
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/auth/LoginPage.vue'),
  },
  {
    path: '/register',
    name: 'Register',
    component: () => import('../views/auth/RegisterPage.vue'),
  },
  {
    path: '/repeaters',
    name: 'Repeaters',
    component: () => import('../views/repeaters/RepeatersPage.vue'),
  },
  {
    path: '/repeaters/new',
    name: 'NewRepeater',
    component: () => import('../views/repeaters/NewRepeaterPage.vue'),
  },
  {
    path: '/repeaters/peers',
    name: 'Peers',
    component: () => import('../views/peers/PeersPage.vue'),
  },
  {
    path: '/repeaters/peers/new',
    name: 'NewPeer',
    component: () => import('../views/peers/NewPeerPage.vue'),
  },
  {
    path: '/talkgroups',
    name: 'Talkgroups',
    component: () => import('../views/talkgroups/TalkgroupsPage.vue'),
  },
  {
    path: '/talkgroups/owned',
    name: 'OwnedTalkgroups',
    component: () => import('../views/talkgroups/OwnedTalkgroupsPage.vue'),
  },
  {
    path: '/admin/repeaters',
    name: 'AdminRepeaters',
    component: () => import('../views/admin/RepeatersPage.vue'),
  },
  {
    path: '/admin/talkgroups',
    name: 'AdminTalkgroups',
    component: () => import('../views/admin/TalkgroupsPage.vue'),
  },
  {
    path: '/admin/talkgroups/new',
    name: 'NewTalkgroups',
    component: () => import('../views/admin/NewTalkgroupsPage.vue'),
  },
  {
    path: '/admin/users',
    name: 'AdminUsers',
    component: () => import('../views/admin/UsersPage.vue'),
  },
  {
    path: '/admin/users/approval',
    name: 'AdminUsersApproval',
    component: () => import('../views/admin/UsersApprovalPage.vue'),
  },
];
