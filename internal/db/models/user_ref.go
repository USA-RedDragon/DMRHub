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

package models

import (
	"encoding/json"
	"fmt"
)

// UserRef is the public form of a User inside another model: its DMR ID
// and callsign only.
type UserRef struct {
	ID       uint   `json:"id"`
	Callsign string `json:"callsign"`
}

// NewUserRef returns the public form of u.
func NewUserRef(u *User) UserRef {
	return UserRef{ID: u.ID, Callsign: u.Callsign}
}

func userRefs(users []User) []UserRef {
	if users == nil {
		return nil
	}
	refs := make([]UserRef, len(users))
	for i := range users {
		refs[i] = NewUserRef(&users[i])
	}
	return refs
}

// MarshalJSON serializes the admins and NCOs of t as UserRefs.
func (t Talkgroup) MarshalJSON() ([]byte, error) {
	type talkgroup Talkgroup
	b, err := json.Marshal(struct {
		talkgroup
		Admins []UserRef `json:"admins"`
		NCOs   []UserRef `json:"ncos"`
	}{talkgroup(t), userRefs(t.Admins), userRefs(t.NCOs)})
	if err != nil {
		return nil, fmt.Errorf("marshal talkgroup: %w", err)
	}
	return b, nil
}

// MarshalJSON serializes the owner of p as a UserRef.
func (p Repeater) MarshalJSON() ([]byte, error) {
	type repeater Repeater
	b, err := json.Marshal(struct {
		repeater
		Owner UserRef `json:"owner"`
	}{repeater(p), NewUserRef(&p.Owner)})
	if err != nil {
		return nil, fmt.Errorf("marshal repeater: %w", err)
	}
	return b, nil
}
