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
	"slices"
	"strings"
	"time"
)

// OAuthGrant is the access that a user allowed a web app: the scopes that
// the app asked for, for as long as it refreshes its tokens. Its ID is in
// every token issued for it, so revoking the grant ends them all.
type OAuthGrant struct {
	ID         string `gorm:"primaryKey"`
	UserID     uint   `gorm:"index"`
	ClientID   string `gorm:"index"`
	Scopes     string
	CreatedAt  time.Time
	LastUsedAt time.Time
	RevokedAt  *time.Time `gorm:"index"`
	// Full is true for a personal token with full account access. It is
	// never stored with a grant of an app.
	Full bool `gorm:"-"`
}

// OAuthCode is an authorization code that a web app trades once, with the
// PKCE verifier of its challenge, for the tokens of a new grant. Any
// attempt to trade it spends it, and trading a spent code again revokes
// the grant it gave.
type OAuthCode struct {
	Hash        string `gorm:"primaryKey"`
	UserID      uint
	ClientID    string
	RedirectURI string
	Scopes      string
	Challenge   string
	ExpiresAt   time.Time `gorm:"index"`
	UsedAt      *time.Time
	GrantID     string
}

// OAuthRefreshToken is a refresh token of a grant. It is used once: using
// it gives a new one, and using it again revokes the grant.
type OAuthRefreshToken struct {
	Hash      string `gorm:"primaryKey"`
	GrantID   string `gorm:"index"`
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// Allows reports whether the grant includes the scope.
func (g OAuthGrant) Allows(scope string) bool {
	return g.Full || slices.Contains(strings.Fields(g.Scopes), scope)
}

// PersonalToken is an API token that a user made for their own scripts.
// It has either Scopes, the same scopes as an app, or Full account
// access. DMRHub keeps only the SHA-256 of its secret, and Prefix, the
// start of the secret, so that the user can tell their tokens apart.
type PersonalToken struct {
	ID     string `gorm:"primaryKey"`
	UserID uint   `gorm:"index"`
	Name   string
	Prefix string
	Hash   string `gorm:"size:64;uniqueIndex"`
	Scopes string
	Full   bool
	// Days is how long the token works from when it was made or rotated,
	// or 0 for no expiry.
	Days       int
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
}

// PersonalGrantPrefix starts the grant ID of a personal token, so that it
// never matches the grant ID of an app.
const PersonalGrantPrefix = "pat:"

// Grant returns the access of the token as the grant of a request.
func (t PersonalToken) Grant() OAuthGrant {
	return OAuthGrant{ID: PersonalGrantPrefix + t.ID, UserID: t.UserID, Scopes: t.Scopes, Full: t.Full, CreatedAt: t.CreatedAt}
}
