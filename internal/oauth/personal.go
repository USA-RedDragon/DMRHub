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

package oauth

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

const (
	// PersonalPrefix starts the secret of every personal token.
	PersonalPrefix = "dmrhub_pat_"
	// MaxPersonalTokens is how many personal tokens a user may have.
	MaxPersonalTokens = 50
	// MaxPersonalNameLength is the longest name of a personal token, in
	// characters.
	MaxPersonalNameLength = 64

	personalSecretLength = 43
	personalHintLength   = 4
)

// ErrTooManyTokens is a new personal token of a user who has
// MaxPersonalTokens already.
var ErrTooManyTokens = errors.New("too many tokens")

// PersonalExpiries are the lifetimes in days that a personal token may
// have. 0 is no expiry.
func PersonalExpiries() []int {
	return []int{30, 90, 365, 0}
}

// PersonalRequest is what a user asks for in a new personal token: a
// name, either the space-separated Scopes or Full account access, and
// Days until it expires (0 for never).
type PersonalRequest struct {
	Name   string
	Scopes string
	Full   bool
	Days   int
}

func (r PersonalRequest) check() (PersonalRequest, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || utf8.RuneCountInString(r.Name) > MaxPersonalNameLength {
		return r, fmt.Errorf("%w: the name must have 1 to %d characters", ErrInvalidRequest, MaxPersonalNameLength)
	}
	if !slices.Contains(PersonalExpiries(), r.Days) {
		return r, fmt.Errorf("%w: the token must expire in 30, 90 or 365 days, or never", ErrInvalidRequest)
	}
	if r.Full {
		if strings.TrimSpace(r.Scopes) != "" {
			return r, fmt.Errorf("%w: a token with full account access has no scopes", ErrInvalidRequest)
		}
		r.Scopes = ""
		return r, nil
	}
	scopes, err := ParseScopes(r.Scopes)
	if err != nil {
		return r, err
	}
	r.Scopes = AuthorizeRequest{Scopes: scopes}.ScopeString()
	return r, nil
}

// CreatePersonal makes a personal token of the user userID and returns it
// with its secret, which DMRHub cannot show again.
func (s *Server) CreatePersonal(userID uint, req PersonalRequest) (models.PersonalToken, string, error) {
	req, err := req.check()
	if err != nil {
		return models.PersonalToken{}, "", err
	}
	var tok models.PersonalToken
	var secret string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&models.PersonalToken{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			return fmt.Errorf("failed to count the tokens: %w", err)
		}
		if n >= MaxPersonalTokens {
			return ErrTooManyTokens
		}
		tok, secret, err = s.savePersonal(tx, userID, req)
		return err
	})
	if err != nil {
		return models.PersonalToken{}, "", fmt.Errorf("failed to make the token: %w", err)
	}
	return tok, secret, nil
}

func (s *Server) savePersonal(tx *gorm.DB, userID uint, req PersonalRequest) (models.PersonalToken, string, error) {
	id, err := random()
	if err != nil {
		return models.PersonalToken{}, "", err
	}
	body, err := random()
	if err != nil {
		return models.PersonalToken{}, "", err
	}
	secret := PersonalPrefix + body
	now := s.Now()
	tok := models.PersonalToken{
		ID:        id,
		UserID:    userID,
		Name:      req.Name,
		Prefix:    secret[:len(PersonalPrefix)+personalHintLength],
		Hash:      hash(secret),
		Scopes:    req.Scopes,
		Full:      req.Full,
		Days:      req.Days,
		CreatedAt: now,
	}
	if req.Days > 0 {
		expires := now.AddDate(0, 0, req.Days)
		tok.ExpiresAt = &expires
	}
	if err := tx.Create(&tok).Error; err != nil {
		return models.PersonalToken{}, "", fmt.Errorf("failed to save the token: %w", err)
	}
	return tok, secret, nil
}

// PersonalTokens returns the personal tokens of the user userID, expired
// ones too, oldest first.
func (s *Server) PersonalTokens(userID uint) ([]models.PersonalToken, error) {
	var tokens []models.PersonalToken
	if err := s.db.Where("user_id = ?", userID).Order("created_at, id").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("failed to list the tokens: %w", err)
	}
	return tokens, nil
}

// RemovePersonal deletes the personal token id of the user userID, which
// ends it at once.
func (s *Server) RemovePersonal(userID uint, id string) error {
	res := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.PersonalToken{})
	if res.Error != nil {
		return fmt.Errorf("failed to delete the token: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RotatePersonal replaces the personal token id of the user userID with a
// new one of the same name, access and lifetime, counted from now, and
// returns it with its secret. The old secret stops working at once.
func (s *Server) RotatePersonal(userID uint, id string) (models.PersonalToken, string, error) {
	var tok models.PersonalToken
	var secret string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var old models.PersonalToken
		if err := tx.Where("id = ? AND user_id = ?", id, userID).Take(&old).Error; err != nil {
			return lookupError(err, ErrNotFound)
		}
		res := tx.Where("id = ?", old.ID).Delete(&models.PersonalToken{})
		if res.Error != nil {
			return fmt.Errorf("failed to delete the old token: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return ErrNotFound
		}
		var err error
		tok, secret, err = s.savePersonal(tx, userID, PersonalRequest{Name: old.Name, Scopes: old.Scopes, Full: old.Full, Days: old.Days})
		return err
	})
	if err != nil {
		return models.PersonalToken{}, "", fmt.Errorf("failed to rotate the token: %w", err)
	}
	return tok, secret, nil
}

// AuthenticatePersonal returns the personal token of secret, which must
// exist and be unexpired.
func (s *Server) AuthenticatePersonal(secret string) (models.PersonalToken, error) {
	body, ok := strings.CutPrefix(secret, PersonalPrefix)
	if !ok || len(body) != personalSecretLength {
		return models.PersonalToken{}, ErrInvalidToken
	}
	var tok models.PersonalToken
	if err := s.db.Where("hash = ?", hash(secret)).Take(&tok).Error; err != nil {
		return models.PersonalToken{}, lookupError(err, ErrInvalidToken)
	}
	now := s.Now()
	if tok.ExpiresAt != nil && !now.Before(*tok.ExpiresAt) {
		return models.PersonalToken{}, ErrInvalidToken
	}
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) >= touchEvery {
		if err := s.db.Model(&models.PersonalToken{}).Where("id = ?", tok.ID).Update("last_used_at", now).Error; err != nil {
			return models.PersonalToken{}, fmt.Errorf("failed to update the token: %w", err)
		}
		tok.LastUsedAt = &now
	}
	return tok, nil
}

func (s *Server) removeUserPersonal(userID uint) error {
	if err := s.db.Where("user_id = ?", userID).Delete(&models.PersonalToken{}).Error; err != nil {
		return fmt.Errorf("failed to delete the tokens of user %d: %w", userID, err)
	}
	return nil
}
