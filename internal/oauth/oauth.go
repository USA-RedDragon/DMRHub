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

// Package oauth lets web apps use a DMRHub account with OAuth 2.0
// authorization codes and PKCE. An app needs no registration: its client
// ID is its origin, and it may only be redirected back to that origin.
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

const (
	// AccessLifetime is how long an access token works.
	AccessLifetime = 15 * time.Minute
	// RefreshLifetime is how long an unused refresh token works.
	RefreshLifetime = 30 * 24 * time.Hour
	// CodeLifetime is how long an authorization code works.
	CodeLifetime = 2 * time.Minute
	// RefreshGrace is how long a spent refresh token still works, so that
	// two tabs of an app that refresh at once both keep their access.
	RefreshGrace = 10 * time.Second
	// SpentTokenLifetime is how long a spent refresh token is kept, so that
	// spending it again ends its grant.
	SpentTokenLifetime = 24 * time.Hour

	accessPrefix  = "dmrhub_at."
	refreshPrefix = "dmrhub_rt."
	touchEvery    = time.Minute
)

var (
	// ErrInvalidRequest is a request that is missing or has a bad parameter.
	ErrInvalidRequest = errors.New("invalid_request")
	// ErrInvalidClient is a client ID that is not an origin, or a redirect
	// that leaves it.
	ErrInvalidClient = errors.New("invalid_client")
	// ErrInvalidScope is an empty or unknown scope.
	ErrInvalidScope = errors.New("invalid_scope")
	// ErrInvalidGrant is a code or refresh token that does not work.
	ErrInvalidGrant = errors.New("invalid_grant")
	// ErrInvalidToken is an access token that does not work.
	ErrInvalidToken = errors.New("invalid_token")
	// ErrNotFound is a grant that the user does not have.
	ErrNotFound = errors.New("not found")
)

// Server issues and checks the tokens of apps.
type Server struct {
	db  *gorm.DB
	key []byte
	// Now returns the current time.
	Now func() time.Time
}

// NewServer returns a Server that keeps its grants in db and signs access
// tokens with a key derived from secret.
func NewServer(db *gorm.DB, secret []byte) *Server {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("DMRHub OAuth access tokens"))
	return &Server{db: db, key: mac.Sum(nil), Now: time.Now}
}

// AuthorizeRequest is a checked request of an app for access.
type AuthorizeRequest struct {
	ClientID    string
	RedirectURI string
	Scopes      []Scope
	Challenge   string
}

// ScopeString returns the scopes of r as the space-separated list of OAuth.
func (r AuthorizeRequest) ScopeString() string {
	names := make([]string, 0, len(r.Scopes))
	for _, s := range r.Scopes {
		names = append(names, s.Name)
	}
	return strings.Join(names, " ")
}

// Tokens are what an app gets for a code or a refresh token.
type Tokens struct {
	Access    string
	Refresh   string
	ExpiresIn time.Duration
	Scope     string
}

// ParseAuthorize checks an authorization request of the app clientID.
func (s *Server) ParseAuthorize(clientID, redirectURI, scope, challenge, method string) (AuthorizeRequest, error) {
	if err := checkClient(clientID, redirectURI); err != nil {
		return AuthorizeRequest{}, err
	}
	scopes, err := ParseScopes(scope)
	if err != nil {
		return AuthorizeRequest{}, err
	}
	if method != "S256" {
		return AuthorizeRequest{}, fmt.Errorf("%w: code_challenge_method must be S256", ErrInvalidRequest)
	}
	if b, err := base64.RawURLEncoding.DecodeString(challenge); err != nil || len(b) != sha256.Size {
		return AuthorizeRequest{}, fmt.Errorf("%w: code_challenge must be a SHA-256 in base64url", ErrInvalidRequest)
	}
	return AuthorizeRequest{ClientID: clientID, RedirectURI: redirectURI, Scopes: scopes, Challenge: challenge}, nil
}

func checkClient(clientID, redirectURI string) error {
	origin, err := Origin(clientID)
	if err != nil || origin != clientID {
		return fmt.Errorf("%w: client_id must be the origin of the app", ErrInvalidClient)
	}
	u, err := url.Parse(redirectURI)
	if err != nil || u.Fragment != "" || strings.Contains(redirectURI, "#") || u.User != nil {
		return fmt.Errorf("%w: redirect_uri is not a URL without a fragment", ErrInvalidClient)
	}
	if u.Scheme+"://"+u.Host != clientID {
		return fmt.Errorf("%w: redirect_uri must be on the origin of the app", ErrInvalidClient)
	}
	return nil
}

// Origin returns the origin of the URL raw, which must use HTTPS unless
// its host is the local machine.
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidClient, err)
	}
	if u.User != nil || u.Host == "" || u.Opaque != "" {
		return "", ErrInvalidClient
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLocal(u.Hostname()) {
			return "", ErrInvalidClient
		}
	default:
		return "", ErrInvalidClient
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Approve records that the user userID allows req and returns the code
// that the app exchanges for tokens.
func (s *Server) Approve(userID uint, req AuthorizeRequest) (string, error) {
	code, err := random()
	if err != nil {
		return "", err
	}
	row := models.OAuthCode{
		Hash:        hash(code),
		UserID:      userID,
		ClientID:    req.ClientID,
		RedirectURI: req.RedirectURI,
		Scopes:      req.ScopeString(),
		Challenge:   req.Challenge,
		ExpiresAt:   s.Now().Add(CodeLifetime),
	}
	if err := s.db.Create(&row).Error; err != nil {
		return "", fmt.Errorf("failed to save the authorization code: %w", err)
	}
	return code, nil
}

// Exchange spends code for tokens. The code works once, for the app and
// redirect it was made for, with the verifier of its challenge. Every
// attempt spends the code, and an attempt with a spent code revokes the
// grant that the code gave, since one of the two holders is not the app.
func (s *Server) Exchange(code, clientID, redirectURI, verifier string) (Tokens, error) {
	var tokens Tokens
	var reused string
	failed := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row models.OAuthCode
		if err := tx.Where("hash = ?", hash(code)).Take(&row).Error; err != nil {
			return lookupError(err, ErrInvalidGrant)
		}
		if row.UsedAt != nil {
			reused = row.GrantID
			return ErrInvalidGrant
		}
		now := s.Now()
		res := tx.Model(&models.OAuthCode{}).Where("hash = ? AND used_at IS NULL", row.Hash).Update("used_at", now)
		if res.Error != nil {
			return fmt.Errorf("failed to spend the authorization code: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return ErrInvalidGrant
		}
		if !now.Before(row.ExpiresAt) ||
			row.ClientID != clientID || row.RedirectURI != redirectURI || !verifies(row.Challenge, verifier) {
			failed = true
			return nil
		}
		grant, err := s.grantFor(tx, row, now)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.OAuthCode{}).Where("hash = ?", row.Hash).Update("grant_id", grant.ID).Error; err != nil {
			return fmt.Errorf("failed to record the grant of the authorization code: %w", err)
		}
		tokens, err = s.issue(tx, grant, now)
		return err
	})
	if reused != "" {
		if err := s.revokeGrant(s.db, reused); err != nil {
			return Tokens{}, err
		}
	}
	if err == nil && failed {
		err = ErrInvalidGrant
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("failed to exchange the code: %w", err)
	}
	return tokens, nil
}

func (s *Server) grantFor(tx *gorm.DB, code models.OAuthCode, now time.Time) (models.OAuthGrant, error) {
	var grant models.OAuthGrant
	err := tx.Where("user_id = ? AND client_id = ? AND revoked_at IS NULL", code.UserID, code.ClientID).Take(&grant).Error
	switch {
	case err == nil:
		grant.Scopes = code.Scopes
		grant.LastUsedAt = now
		if err := tx.Model(&grant).Updates(map[string]any{"scopes": grant.Scopes, "last_used_at": now}).Error; err != nil {
			return grant, fmt.Errorf("failed to update the grant: %w", err)
		}
		return grant, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		id, err := random()
		if err != nil {
			return grant, err
		}
		grant = models.OAuthGrant{ID: id, UserID: code.UserID, ClientID: code.ClientID, Scopes: code.Scopes, CreatedAt: now, LastUsedAt: now}
		if err := tx.Create(&grant).Error; err != nil {
			return grant, fmt.Errorf("failed to save the grant: %w", err)
		}
		return grant, nil
	default:
		return grant, fmt.Errorf("failed to find the grant: %w", err)
	}
}

// Refresh spends the refresh token of the app clientID for new tokens. A
// refresh token works once, or again within RefreshGrace: spending it
// later ends the grant, since one of the two holders is not the app.
func (s *Server) Refresh(refresh, clientID string) (Tokens, error) {
	var tokens Tokens
	var reused string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row models.OAuthRefreshToken
		if err := tx.Where("hash = ?", hash(refresh)).Take(&row).Error; err != nil {
			return lookupError(err, ErrInvalidGrant)
		}
		now := s.Now()
		grant, err := activeGrant(tx, row.GrantID)
		if err != nil {
			return err
		}
		if grant.ClientID != clientID {
			return ErrInvalidGrant
		}
		if !now.Before(row.ExpiresAt) {
			return ErrInvalidGrant
		}
		if row.UsedAt != nil {
			if now.Sub(*row.UsedAt) >= RefreshGrace {
				reused = grant.ID
				return ErrInvalidGrant
			}
		} else if err := tx.Model(&models.OAuthRefreshToken{}).Where("hash = ? AND used_at IS NULL", row.Hash).Update("used_at", now).Error; err != nil {
			return fmt.Errorf("failed to spend the refresh token: %w", err)
		}
		if err := tx.Model(&grant).Update("last_used_at", now).Error; err != nil {
			return fmt.Errorf("failed to update the grant: %w", err)
		}
		tokens, err = s.issue(tx, grant, now)
		return err
	})
	if reused != "" {
		if err := s.revokeGrant(s.db, reused); err != nil {
			return Tokens{}, err
		}
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("failed to refresh: %w", err)
	}
	return tokens, nil
}

func (s *Server) issue(tx *gorm.DB, grant models.OAuthGrant, now time.Time) (Tokens, error) {
	secret, err := random()
	if err != nil {
		return Tokens{}, err
	}
	refresh := refreshPrefix + secret
	row := models.OAuthRefreshToken{Hash: hash(refresh), GrantID: grant.ID, ExpiresAt: now.Add(RefreshLifetime)}
	if err := tx.Create(&row).Error; err != nil {
		return Tokens{}, fmt.Errorf("failed to save the refresh token: %w", err)
	}
	access, err := s.sign(claims{Grant: grant.ID, Expires: now.Add(AccessLifetime).Unix()})
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{Access: access, Refresh: refresh, ExpiresIn: AccessLifetime, Scope: grant.Scopes}, nil
}

type claims struct {
	Grant   string `json:"g"`
	Expires int64  `json:"e"`
}

func (s *Server) sign(c claims) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("failed to encode the access token: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return accessPrefix + payload + "." + s.mac(payload), nil
}

func (s *Server) mac(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) parse(token string) (claims, bool) {
	rest, ok := strings.CutPrefix(token, accessPrefix)
	if !ok {
		return claims{}, false
	}
	payload, sig, ok := strings.Cut(rest, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.mac(payload))) {
		return claims{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return claims{}, false
	}
	var c claims
	if err := json.Unmarshal(body, &c); err != nil || c.Grant == "" {
		return claims{}, false
	}
	return c, true
}

// Authenticate returns the grant of the access token, which must be
// unexpired and of a grant that the user did not remove.
func (s *Server) Authenticate(access string) (models.OAuthGrant, error) {
	c, ok := s.parse(access)
	now := s.Now()
	if !ok || now.Unix() >= c.Expires {
		return models.OAuthGrant{}, ErrInvalidToken
	}
	grant, err := activeGrant(s.db, c.Grant)
	if err != nil {
		if errors.Is(err, ErrInvalidGrant) {
			return models.OAuthGrant{}, ErrInvalidToken
		}
		return models.OAuthGrant{}, err
	}
	if now.Sub(grant.LastUsedAt) >= touchEvery {
		if err := s.db.Model(&grant).Update("last_used_at", now).Error; err != nil {
			return models.OAuthGrant{}, fmt.Errorf("failed to update the grant: %w", err)
		}
	}
	return grant, nil
}

// Revoke ends the grant of the access or refresh token of the app
// clientID and returns that grant. Tokens that are unknown or of another
// app are ignored, and the returned bool is false.
func (s *Server) Revoke(token, clientID string) (models.OAuthGrant, bool, error) {
	var grantID string
	if c, ok := s.parse(token); ok {
		grantID = c.Grant
	} else {
		var row models.OAuthRefreshToken
		if err := s.db.Where("hash = ?", hash(token)).Take(&row).Error; err != nil {
			return models.OAuthGrant{}, false, ignoreNotFound(err)
		}
		grantID = row.GrantID
	}
	grant, err := activeGrant(s.db, grantID)
	if err != nil {
		if errors.Is(err, ErrInvalidGrant) {
			return models.OAuthGrant{}, false, nil
		}
		return models.OAuthGrant{}, false, err
	}
	if grant.ClientID != clientID {
		return models.OAuthGrant{}, false, nil
	}
	if err := s.revokeGrant(s.db, grant.ID); err != nil {
		return models.OAuthGrant{}, false, err
	}
	return grant, true, nil
}

// Grants returns the apps that the user userID allowed.
func (s *Server) Grants(userID uint) ([]models.OAuthGrant, error) {
	var grants []models.OAuthGrant
	if err := s.db.Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at").Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("failed to list the grants: %w", err)
	}
	return grants, nil
}

// RemoveGrant ends the grant id of the user userID.
func (s *Server) RemoveGrant(userID uint, id string) error {
	grant, err := activeGrant(s.db, id)
	if err != nil {
		if errors.Is(err, ErrInvalidGrant) {
			return ErrNotFound
		}
		return err
	}
	if grant.UserID != userID {
		return ErrNotFound
	}
	return s.revokeGrant(s.db, id)
}

// RevokeUserGrants ends every grant of the user userID, so that no app
// keeps access after a password reset.
func (s *Server) RevokeUserGrants(userID uint) error {
	if err := s.db.Model(&models.OAuthGrant{}).Where("user_id = ? AND revoked_at IS NULL", userID).Update("revoked_at", s.Now()).Error; err != nil {
		return fmt.Errorf("failed to revoke the grants of user %d: %w", userID, err)
	}
	return nil
}

func (s *Server) revokeGrant(db *gorm.DB, id string) error {
	now := s.Now()
	if err := db.Model(&models.OAuthGrant{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error; err != nil {
		return fmt.Errorf("failed to revoke the grant: %w", err)
	}
	return nil
}

// Prune deletes the codes and refresh tokens that can no longer work and
// the grants that were removed. It keeps a spent code that gave a grant,
// and a spent refresh token, for SpentTokenLifetime, so that spending it
// again ends its grant.
func (s *Server) Prune() error {
	now := s.Now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		revoked := tx.Model(&models.OAuthGrant{}).Select("id").Where("revoked_at IS NOT NULL")
		if err := tx.Where("(expires_at <= ? AND (grant_id IS NULL OR grant_id = '')) OR used_at <= ? OR grant_id IN (?)", now, now.Add(-SpentTokenLifetime), revoked).
			Delete(&models.OAuthCode{}).Error; err != nil {
			return fmt.Errorf("failed to prune codes: %w", err)
		}
		if err := tx.Where("expires_at <= ? OR used_at <= ? OR grant_id IN (?)", now, now.Add(-SpentTokenLifetime), revoked).
			Delete(&models.OAuthRefreshToken{}).Error; err != nil {
			return fmt.Errorf("failed to prune refresh tokens: %w", err)
		}
		if err := tx.Where("revoked_at IS NOT NULL").Delete(&models.OAuthGrant{}).Error; err != nil {
			return fmt.Errorf("failed to prune grants: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to prune OAuth data: %w", err)
	}
	return nil
}

// PeriodicPrune runs Prune every interval until ctx ends.
func (s *Server) PeriodicPrune(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Prune(); err != nil {
				slog.Error("Failed to prune OAuth data", "error", err)
			}
		}
	}
}

func activeGrant(db *gorm.DB, id string) (models.OAuthGrant, error) {
	var grant models.OAuthGrant
	if err := db.Where("id = ? AND revoked_at IS NULL", id).Take(&grant).Error; err != nil {
		return grant, lookupError(err, ErrInvalidGrant)
	}
	return grant, nil
}

func lookupError(err, notFound error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notFound
	}
	return fmt.Errorf("failed to look up the token: %w", err)
}

func ignoreNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return fmt.Errorf("failed to look up the token: %w", err)
}

func verifies(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge))
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func random() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to make a random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
