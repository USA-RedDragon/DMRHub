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

// Package airauth keeps the on-air codes of the users: the TOTP secret
// (RFC 6238) of each user, the check of a code that a radio sends as a
// private call to 9 and the 6 digits of the code, and the unlocks that a
// right code gives a radio ID on one repeater slot. Its state is in the
// database and the shared KV store, so every replica sees it. Nothing in
// it stores or logs a code.
package airauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1, as every authenticator app does
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"net/url"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
)

const (
	// Period is the length of one time step of a code.
	Period = 30 * time.Second
	// Digits is the number of digits of a code.
	Digits = 6
	// secretLen is the length of a secret, 160 bits as RFC 4226 advises.
	secretLen = 20
	codeMod   = 1000000
)

func b32() *base32.Encoding { return base32.StdEncoding.WithPadding(base32.NoPadding) }

// NewSecret returns a new random secret.
func NewSecret() []byte {
	b := make([]byte, secretLen)
	_, _ = rand.Read(b)
	return b
}

// EncodeSecret returns secret in base32, as an authenticator app takes it.
func EncodeSecret(secret []byte) string {
	return b32().EncodeToString(secret)
}

// Step returns the time step of t.
func Step(t time.Time) int64 {
	return t.Unix() / int64(Period/time.Second)
}

// Code returns the code of secret for the time step step.
func Code(secret []byte, step int64) uint32 {
	mac := hmac.New(sha1.New, secret)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // a time step is never negative
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return v % codeMod
}

// Match returns the time step of now, the one before or the one after
// that has code, and false when none has it. It compares every step in
// the same time.
func Match(secret []byte, code uint32, now time.Time) (int64, bool) {
	t := Step(now)
	var found int64
	ok := 0
	for _, s := range [...]int64{t - 1, t, t + 1} {
		eq := subtle.ConstantTimeEq(int32(Code(secret, s)), int32(code)) //nolint:gosec // a code is below 10^6
		if eq == 1 && ok == 0 {
			found = s
		}
		ok |= eq
	}
	return found, ok == 1
}

// CodeOfCall returns the code that a private call to dst carries, and
// false when dst is not an unlock ID.
func CodeOfCall(dst uint) (uint32, bool) {
	if !dmrconst.IsUnlockID(dst) {
		return 0, false
	}
	return uint32(dst - dmrconst.UnlockFirst), true //nolint:gosec // below 10^6
}

// ParseCode returns the code that a user typed, and false when it is not 6
// digits. Spaces are left out.
func ParseCode(s string) (uint32, bool) {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) != Digits {
		return 0, false
	}
	var v uint32
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + uint32(c-'0')
	}
	return v, true
}

// URI returns the otpauth URI of secret that an authenticator app scans,
// with issuer as the name of the network and account as the name of the
// user.
func URI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", EncodeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}
