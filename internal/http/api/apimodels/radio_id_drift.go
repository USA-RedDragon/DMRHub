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

package apimodels

import "time"

// RadioIDDrift is a radio ID of a user, their account ID or one they
// added, that RadioID does not list under the callsign of the user.
type RadioIDDrift struct {
	RadioID  uint   `json:"radio_id"`
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	// Callsign is the callsign of the user on DMRHub.
	Callsign string `json:"callsign"`
	// RadioIDCallsign is the callsign that RadioID lists for the radio
	// ID, or empty when RadioID does not have the radio ID.
	RadioIDCallsign string `json:"radio_id_callsign"`
	// CanFix reports that a fix applies: RadioID lists the account ID of
	// the user, and this radio ID, under one new callsign.
	CanFix    bool      `json:"can_fix"`
	FoundAt   time.Time `json:"found_at"`
	CheckedAt time.Time `json:"checked_at"`
	// CanNotify reports that a fix can email the user: email is on and
	// the user verified an address.
	CanNotify bool `json:"can_notify"`
}

// RadioIDDriftList is a page of RadioID drifts.
type RadioIDDriftList struct {
	Total  int64          `json:"total"`
	Drifts []RadioIDDrift `json:"drifts"`
}

// RadioIDDriftFix asks to give the user of a radio ID the callsign that
// RadioID lists for their account ID.
type RadioIDDriftFix struct {
	// Notify asks to email the user about the new callsign.
	Notify bool `json:"notify"`
}

// RadioIDDriftFixed is the answer to a RadioIDDriftFix.
type RadioIDDriftFixed struct {
	Message  string `json:"message"`
	Callsign string `json:"callsign"`
	// Notified reports that an email to the user is on its way.
	Notified bool `json:"notified"`
}
