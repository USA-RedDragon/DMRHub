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

package middleware

// The errors of a request that a signed-in user lacks the right for. The
// middleware answers them with 403; a signed-out, unapproved or suspended
// user gets 401 with msgAuthFailed instead.
const (
	MsgNotAdmin                 = "You must be an admin"
	MsgNotSuperAdmin            = "You must be a super admin"
	MsgNotAdminOrTalkgroupOwner = "You must be an admin or a talkgroup admin"
	MsgNotSelf                  = "You can only do this for your own account"
	MsgNotPeerOwner             = "You do not own this peer"
	MsgNotTalkgroupAdmin        = "You are not an admin of this talkgroup"
	MsgNotTalkgroupNCO          = "You are not an admin or NCO of this talkgroup"
)
