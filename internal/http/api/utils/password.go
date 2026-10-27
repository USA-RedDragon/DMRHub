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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is the fewest characters that a new password may have.
// A password set before the minimum still logs in until it is changed.
const MinPasswordLength = 8

// MsgShortPassword is the answer to a new password that is too short.
const MsgShortPassword = "The password must have at least 8 characters."

// ShortPassword reports whether password has fewer than MinPasswordLength
// characters.
func ShortPassword(password string) bool {
	return utf8.RuneCountInString(password) < MinPasswordLength
}

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
}

var (
	ErrInvalidHash         = errors.New("the encoded hash is not in the correct format")
	ErrIncompatibleVersion = errors.New("incompatible version of argon2")
	ErrNoRandom            = errors.New("no random source available")
)

// argon2Slots bounds how many argon2 derivations run at once. Each derivation
// allocates its whole memory cost (64 MiB) up front, so a burst of concurrent
// logins could otherwise grow the heap without limit.
var argon2Slots = make(chan struct{}, argon2Concurrency()) //nolint:gochecknoglobals

// argon2Concurrency returns the number of argon2 derivations allowed to run at
// once. Each derivation already uses parallelism threads, so a few slots keep
// every CPU busy. On 32-bit targets the garbage collector falls behind
// overlapping derivations and the heap grows until the address space runs out,
// so derivations run one at a time there.
func argon2Concurrency() int {
	if strconv.IntSize == 32 {
		return 1
	}
	return max(2, runtime.GOMAXPROCS(0)/parallelism)
}

// idKey is argon2.IDKey gated by argon2Slots.
func idKey(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
	argon2Slots <- struct{}{}
	defer func() { <-argon2Slots }()
	return argon2.IDKey(password, salt, time, memory, threads, keyLen)
}

var randRead = rand.Read //nolint:gochecknoglobals

const (
	dummyInput = "__dummy_timing_protection__"
	dummySalt  = "__dummy__"
)

var (
	dummyHash     string    //nolint:gochecknoglobals
	dummyHashOnce sync.Once //nolint:gochecknoglobals
)

// DummyHash returns a valid argon2id hash made by the same code and with the
// same parameters as real password hashes, so VerifyPassword takes the same
// time on it and does not leak whether a user account exists. It uses a fixed
// salt, so it needs no random source and cannot fail.
func DummyHash() string {
	dummyHashOnce.Do(func() {
		dummyHash = encodeHash(dummyInput+dummySalt, make([]byte, saltLength))
	})
	return dummyHash
}

func HashPassword(password string, salt string) (string, error) {
	argonSalt := make([]byte, saltLength)
	if _, err := randRead(argonSalt); err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}
	return encodeHash(password+salt, argonSalt), nil
}

func encodeHash(password string, salt []byte) string {
	key := idKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(key)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, iterations, parallelism, b64Salt, b64Hash)
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
	otherHash := idKey([]byte(password+pwsalt), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

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
const allowedSpecial = "-_."

const (
	peerPasswordLen     = 13
	repeaterPasswordLen = 12
)

// NewPeerPassword returns a password for an OpenBridge peer. The other server
// keeps it in its configuration, such as the PASSPHRASE of FreeDMR or HBlink3.
func NewPeerPassword() (string, error) {
	return RandomPassword(peerPasswordLen, 1, 2)
}

// NewRepeaterPassword returns a password for an MMDVM repeater or hotspot.
func NewRepeaterPassword() (string, error) {
	return RandomPassword(repeaterPasswordLen, 4, 0)
}

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
