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

package config

import (
	"errors"
	"time"
)

const (
	fieldSessionLength = "http.session-length"
	fieldRegistration  = "registration"
	fieldVisibility    = "visibility"
)

// ErrInvalidVisibility indicates a visibility that is not public, login or
// hide-names.
var ErrInvalidVisibility = errors.New("visibility must be public, login or hide-names")

// Mode returns the visibility, with public for empty.
func (v Visibility) Mode() Visibility {
	if v == "" {
		return VisibilityPublic
	}
	return v
}

// ErrInvalidRegistration indicates a registration mode that is not open,
// invite or closed.
var ErrInvalidRegistration = errors.New("registration must be open, invite or closed")

// Mode returns the registration mode, with open for empty.
func (r Registration) Mode() Registration {
	if r == "" {
		return RegistrationOpen
	}
	return r
}

// MinSessionLength is the shortest session length.
const MinSessionLength = time.Minute

// DefaultSessionLength is the session length that 0 stands for.
const DefaultSessionLength = 30 * 24 * time.Hour

// ErrInvalidSessionLength indicates a session length under a minute.
var ErrInvalidSessionLength = errors.New("the session length must be at least 1m, such as 720h")

// Session returns how long a login lasts: the session length, or
// DefaultSessionLength for 0.
func (h HTTP) Session() time.Duration {
	if h.SessionLength == 0 {
		return DefaultSessionLength
	}
	return h.SessionLength
}

func (c Config) accountErrors() (errs []ValidationError) {
	if c.HTTP.SessionLength != 0 && c.HTTP.SessionLength < MinSessionLength {
		errs = append(errs, ValidationError{Field: fieldSessionLength, Err: ErrInvalidSessionLength})
	}
	switch c.Registration.Mode() {
	case RegistrationOpen, RegistrationInvite, RegistrationClosed:
	default:
		errs = append(errs, ValidationError{Field: fieldRegistration, Err: ErrInvalidRegistration})
	}
	switch c.Visibility.Mode() {
	case VisibilityPublic, VisibilityLogin, VisibilityHideNames:
	default:
		errs = append(errs, ValidationError{Field: fieldVisibility, Err: ErrInvalidVisibility})
	}
	return errs
}
