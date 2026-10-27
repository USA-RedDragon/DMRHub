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

package liveconfig

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const generatedBytes = 32

// generatedPaths are the secrets that DMRHub makes, once, when no config
// file, environment variable or flag sets them.
func generatedPaths() []string {
	return []string{"secret", "password-salt"}
}

// generatedSecret returns the secret name that DMRHub made, and makes it
// first if no replica did. Every replica gets the same value.
func generatedSecret(ctx context.Context, db *gorm.DB, name string) (string, error) {
	var b [generatedBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to make the %s: %w", name, err)
	}
	made := models.GeneratedSecret{Name: name, Value: base64.RawURLEncoding.EncodeToString(b[:])}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&made).Error; err != nil {
		return "", fmt.Errorf("failed to store the %s: %w", name, err)
	}
	var got models.GeneratedSecret
	if err := db.WithContext(ctx).Where("name = ?", name).Take(&got).Error; err != nil {
		return "", fmt.Errorf("failed to read the %s: %w", name, err)
	}
	return got.Value, nil
}

// loadGenerated finds, or makes, each secret of generatedPaths that the
// deployment leaves empty.
func (l *Live) loadGenerated(ctx context.Context) error {
	generated := map[string]string{}
	for _, k := range l.keys {
		if !isGenerated(k.Path) {
			continue
		}
		if _, locked := l.opts.Locks[k.Path]; locked {
			continue
		}
		if v, err := k.Value(l.opts.Base); err != nil || string(v) != `""` {
			continue
		}
		v, err := generatedSecret(ctx, l.opts.DB, k.Path)
		if err != nil {
			return err
		}
		generated[k.Path] = v
	}
	l.mu.Lock()
	l.generated = generated
	l.mu.Unlock()
	return nil
}

func isGenerated(path string) bool {
	for _, p := range generatedPaths() {
		if p == path {
			return true
		}
	}
	return false
}
