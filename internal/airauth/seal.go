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

package airauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// ErrSealed is the error of a sealed secret that the key cannot open, such
// as after a change of the hub secret.
var ErrSealed = errors.New("the on-air code secret cannot be opened")

const sealPrefix = "v1:"

// KeyOf returns the key that seals the secrets of the on-air codes, made
// from the secret and the password salt of cfg. Every replica makes the
// same key from the same config.
func KeyOf(cfg *config.Config) []byte {
	if cfg == nil || cfg.Secret == "" {
		return nil
	}
	key, err := hkdf.Key(sha256.New, cfg.GetDerivedSecret(), nil, "DMRHub on-air code", 32)
	if err != nil {
		return nil
	}
	return key
}

func aead(key []byte) (cipher.AEAD, error) {
	if len(key) == 0 {
		return nil, ErrSealed
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("make the cipher of the on-air code: %w", err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("make the cipher of the on-air code: %w", err)
	}
	return g, nil
}

// Seal returns secret sealed with key for the user userID. Only Open with
// the same key and user opens it.
func Seal(key []byte, userID uint, secret []byte) (string, error) {
	g, err := aead(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce)
	out := g.Seal(nonce, nonce, secret, []byte(strconv.FormatUint(uint64(userID), 10)))
	return sealPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// Open returns the secret that Seal sealed with key for the user userID.
func Open(key []byte, userID uint, sealed string) ([]byte, error) {
	g, err := aead(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < len(sealPrefix) || sealed[:len(sealPrefix)] != sealPrefix {
		return nil, ErrSealed
	}
	b, err := base64.RawStdEncoding.DecodeString(sealed[len(sealPrefix):])
	if err != nil || len(b) < g.NonceSize() {
		return nil, ErrSealed
	}
	secret, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte(strconv.FormatUint(uint64(userID), 10)))
	if err != nil {
		return nil, ErrSealed
	}
	return secret, nil
}
