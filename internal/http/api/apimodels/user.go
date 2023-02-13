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

package apimodels

import "regexp"

const minUsernameLength = 3
const maxUsernameLength = 20

type UserRegistration struct {
	DMRId    uint   `json:"id" binding:"required"`
	Callsign string `json:"callsign" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (r *UserRegistration) IsValidUsername() (bool, string) {
	if len(r.Username) < minUsernameLength {
		return false, "Username must be at least 3 characters"
	}
	if len(r.Username) > maxUsernameLength {
		return false, "Username must be less than 20 characters"
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`).MatchString(r.Username) {
		return false, "Username must be alphanumeric, _, -, or ."
	}
	return true, ""
}

type UserPatch struct {
	Callsign string `json:"callsign"`
	Username string `json:"username"`
	Password string `json:"password"`
}
