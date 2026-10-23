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

// Package sessionstore keeps the HTTP sessions in the database.
package sessionstore

import (
	"context"
	"encoding/base32"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"gorm.io/gorm"
)

const (
	defaultMaxAge = 60 * 60 * 24 * 30
	sessionIDLen  = 32
)

// Store is a gin session store on a gorm database. It is safe for
// concurrent use; Options must be called before the store serves requests.
type Store struct {
	db     *gorm.DB
	codecs []securecookie.Codec
	opts   sessions.Options
}

// New returns a Store on the sessions table of db that signs the session
// cookies with keyPairs.
func New(db *gorm.DB, keyPairs ...[]byte) *Store {
	st := &Store{
		db:     db,
		codecs: securecookie.CodecsFromPairs(keyPairs...),
		opts:   sessions.Options{Path: "/", MaxAge: defaultMaxAge},
	}
	st.setCodecMaxAge(defaultMaxAge)
	return st
}

// Options sets the options of new sessions.
func (st *Store) Options(options ginsessions.Options) {
	st.opts = *options.ToGorillaOptions()
	st.setCodecMaxAge(st.opts.MaxAge)
}

func (st *Store) setCodecMaxAge(age int) {
	for _, codec := range st.codecs {
		if sc, ok := codec.(*securecookie.SecureCookie); ok {
			sc.MaxAge(age)
		}
	}
}

// Get returns the session name of r from the registry of r.
func (st *Store) Get(r *http.Request, name string) (*sessions.Session, error) {
	session, err := sessions.GetRegistry(r).Get(st, name)
	if err != nil {
		return session, fmt.Errorf("failed to get session %s: %w", name, err)
	}
	return session, nil
}

// New returns the session name of r, loaded from the database when the
// cookie of r names a live one, or a new empty session.
func (st *Store) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(st, name)
	opts := st.opts
	session.Options = &opts
	session.IsNew = true
	if row := st.fromCookie(r, name); row != nil {
		if err := securecookie.DecodeMulti(name, row.Data, &session.Values, st.codecs...); err == nil {
			session.ID = row.ID
			session.IsNew = false
		}
	}
	return session, nil
}

// Save writes session to the database and sets its cookie on w. A session
// with a negative MaxAge is deleted.
func (st *Store) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	row := st.fromCookie(r, session.Name())
	if session.Options.MaxAge < 0 {
		if row != nil {
			if err := st.db.Delete(row).Error; err != nil {
				return fmt.Errorf("failed to delete a session: %w", err)
			}
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	data, err := securecookie.EncodeMulti(session.Name(), session.Values, st.codecs...)
	if err != nil {
		return fmt.Errorf("failed to encode a session: %w", err)
	}
	now := time.Now()
	expires := now.Add(time.Duration(session.Options.MaxAge) * time.Second)
	if row == nil {
		session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(sessionIDLen)), "=")
		row = &models.Session{ID: session.ID, Data: data, CreatedAt: now, UpdatedAt: now, ExpiresAt: expires}
		if err := st.db.Create(row).Error; err != nil {
			return fmt.Errorf("failed to create a session: %w", err)
		}
	} else {
		row.Data = data
		row.UpdatedAt = now
		row.ExpiresAt = expires
		if err := st.db.Save(row).Error; err != nil {
			return fmt.Errorf("failed to save a session: %w", err)
		}
	}

	id, err := securecookie.EncodeMulti(session.Name(), row.ID, st.codecs...)
	if err != nil {
		return fmt.Errorf("failed to encode a session cookie: %w", err)
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), id, session.Options))
	return nil
}

func (st *Store) fromCookie(r *http.Request, name string) *models.Session {
	cookie, err := r.Cookie(name)
	if err != nil {
		return nil
	}
	var id string
	if err := securecookie.DecodeMulti(name, cookie.Value, &id, st.codecs...); err != nil {
		return nil
	}
	var row models.Session
	res := st.db.WithContext(r.Context()).Where("id = ? AND expires_at > ?", id, time.Now()).Limit(1).Find(&row)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil
	}
	return &row
}

// Cleanup deletes the expired sessions.
func (st *Store) Cleanup(ctx context.Context) {
	if err := st.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&models.Session{}).Error; err != nil {
		slog.Error("Failed to delete expired sessions", "error", err)
	}
}

// PeriodicCleanup runs Cleanup every interval until ctx ends.
func (st *Store) PeriodicCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st.Cleanup(ctx)
		}
	}
}
