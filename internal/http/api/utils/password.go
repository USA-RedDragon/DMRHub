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

package utils

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

const (
	memory      = 64 * 1024
	iterations  = 3
	parallelism = 8
	saltLength  = 16
	keyLength   = 32
)

type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
	salt        []byte
}

var (
	ErrInvalidHash         = errors.New("the encoded hash is not in the correct format")
	ErrIncompatibleVersion = errors.New("incompatible version of argon2")
	ErrNoRandom            = errors.New("no random source available")
)

var (
	dummyHash     string    //nolint:gochecknoglobals
	dummyHashOnce sync.Once //nolint:gochecknoglobals
)

// DummyHash returns a valid argon2id hash generated with the exact same code
// path and parameters as real password hashes. Using HashPassword (rather than
// a hand-crafted format string) guarantees identical salt length, key length,
// and encoding, which eliminates subtle timing differences in VerifyPassword
// that could leak whether a user account exists.
func DummyHash() string {
	dummyHashOnce.Do(func() {
		var err error
		dummyHash, err = HashPassword("__dummy_timing_protection__", "__dummy__")
		if err != nil {
			panic("failed to generate dummy hash for timing protection: " + err.Error())
		}
	})
	return dummyHash
}

func HashPassword(password string, salt string) (string, error) {
	var params = argon2Params{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		saltLength:  saltLength,
		keyLength:   keyLength,
		salt:        make([]byte, saltLength),
	}
	// Fill the salt with cryptographically secure random bytes.
	_, err := rand.Read(params.salt)
	if err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	bytes := argon2.IDKey([]byte(password+salt), params.salt, params.iterations, params.memory, params.parallelism, params.keyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(params.salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(bytes)

	// Return a string using the standard encoded hash representation.
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, params.memory, params.iterations, params.parallelism, b64Salt, b64Hash), nil
}

func VerifyPassword(password, compareHash string, pwsalt string) (bool, error) {
	vals := strings.Split(compareHash, "$")
	const argon2Vals = 6
	if len(vals) != argon2Vals {
		return false, ErrInvalidHash
	}

	var version int
	_, err := fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}

	p := &argon2Params{}
	_, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism)
	if err != nil {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(vals[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	saltLen := len(salt)
	if saltLen > math.MaxInt32 {
		return false, ErrInvalidHash
	}
	p.saltLength = uint32(saltLen)

	hash, err := base64.RawStdEncoding.Strict().DecodeString(vals[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	hashLen := len(hash)
	if hashLen > math.MaxInt32 {
		return false, ErrInvalidHash
	}
	p.keyLength = uint32(hashLen)

	// Derive the key from the other password using the same parameters.
	otherHash := argon2.IDKey([]byte(password+pwsalt), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

	// Check that the contents of the hashed passwords are identical. Note
	// that we are using the subtle.ConstantTimeCompare() function for this
	// to help prevent timing attacks.
	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

const allowedChars = "bcdefghjkmnopqrstuvwxyzBCDEFGHJKMNPQRSTUVWXYZ"
const allowedNumbers = "2356789"
const allowedSpecial = "!@#$%^&*-_"

func RandomPassword(length int, minNumbers, minSpecial int) (string, error) {
	if minNumbers+minSpecial > length {
		return "", ErrNoRandom
	}

	b := make([]byte, length)

	// Build a list of all indices, then shuffle and pick distinct positions
	// for number and special characters to avoid overwrites.
	indices := make([]int, length)
	for i := range indices {
		indices[i] = i
	}
	// Fisher-Yates shuffle
	for i := length - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", ErrNoRandom
		}
		j := int(jBig.Int64())
		indices[i], indices[j] = indices[j], indices[i]
	}

	numberIndices := make(map[int]bool, minNumbers)
	specialIndices := make(map[int]bool, minSpecial)
	for i := 0; i < minNumbers; i++ {
		numberIndices[indices[i]] = true
	}
	for i := 0; i < minSpecial; i++ {
		specialIndices[indices[minNumbers+i]] = true
	}

	for i := 0; i < length; i++ {
		var charset string
		switch {
		case numberIndices[i]:
			charset = allowedNumbers
		case specialIndices[i]:
			charset = allowedSpecial
		default:
			charset = allowedChars
		}
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", ErrNoRandom
		}
		b[i] = charset[idx.Int64()]
	}

	return string(b), nil
}

// RandomHexString generates a cryptographically random hex string of the given length.
// The length is the number of hex characters (each character is 4 bits).
func RandomHexString(hexLen int) (string, error) {
	byteLen := (hexLen + 1) / 2
	b := make([]byte, byteLen)
	_, err := rand.Read(b)
	if err != nil {
		return "", ErrNoRandom
	}
	return hex.EncodeToString(b)[:hexLen], nil
}
