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

package utils_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
)

func TestHashPasswordReturnsNoErrorOnSuccess(t *testing.T) {
	// Regression test: HashPassword must return a non-nil error on failure
	// instead of silently producing a hash with a zero salt.
	// See: https://github.com/USA-RedDragon/DMRHub report.md "HashPassword silently continues on failure"
	t.Parallel()
	hash, err := utils.HashPassword("testpassword", "salt")
	if err != nil {
		t.Fatalf("HashPassword returned unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("Expected non-empty hash on success")
	}
	// Verify the hash can be verified back
	match, err := utils.VerifyPassword("testpassword", hash, "salt")
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !match {
		t.Error("Expected hash to verify correctly")
	}
}

func TestHashPasswordNonEmpty(t *testing.T) {
	t.Parallel()
	hash, err := utils.HashPassword("testpassword", "salt")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("Expected non-empty hash")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("Expected argon2id hash prefix, got %s", hash)
	}
}

func TestHashPasswordDifferentSalts(t *testing.T) {
	t.Parallel()
	hash1, err := utils.HashPassword("password", "salt1")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	hash2, err := utils.HashPassword("password", "salt2")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash1 == hash2 {
		t.Error("Expected different hashes (random salt should differ)")
	}
}

func TestVerifyPasswordCorrect(t *testing.T) {
	t.Parallel()
	salt := "testsalt"
	password := "mypassword"
	hash, err := utils.HashPassword(password, salt)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	match, err := utils.VerifyPassword(password, hash, salt)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !match {
		t.Error("Expected password to match")
	}
}

func TestVerifyPasswordIncorrect(t *testing.T) {
	t.Parallel()
	salt := "testsalt"
	hash, err := utils.HashPassword("correctPassword", salt)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	match, err := utils.VerifyPassword("wrongPassword", hash, salt)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if match {
		t.Error("Expected password to not match")
	}
}

func TestVerifyPasswordInvalidHash(t *testing.T) {
	t.Parallel()
	_, err := utils.VerifyPassword("password", "not-a-valid-hash", "salt")
	if err == nil {
		t.Error("Expected error for invalid hash format")
	}
}

func TestVerifyPasswordEmptyHash(t *testing.T) {
	t.Parallel()
	_, err := utils.VerifyPassword("password", "", "salt")
	if err == nil {
		t.Error("Expected error for empty hash")
	}
}

func TestRandomPasswordLength(t *testing.T) {
	t.Parallel()
	pw, err := utils.RandomPassword(20, 2, 2)
	if err != nil {
		t.Fatalf("RandomPassword failed: %v", err)
	}
	if len(pw) != 20 {
		t.Errorf("Expected length 20, got %d", len(pw))
	}
}

func TestRandomPasswordContainsNumbers(t *testing.T) {
	t.Parallel()
	for i := 0; i < 10; i++ {
		pw, err := utils.RandomPassword(30, 5, 0)
		if err != nil {
			t.Fatalf("RandomPassword failed: %v", err)
		}
		hasDigit := false
		for _, c := range pw {
			if unicode.IsDigit(c) {
				hasDigit = true
				break
			}
		}
		if !hasDigit {
			t.Errorf("Expected password to contain digits: %s", pw)
		}
	}
}

func TestRandomPasswordContainsSpecial(t *testing.T) {
	t.Parallel()
	specialChars := "!@#$%^&*-_"
	for i := 0; i < 10; i++ {
		pw, err := utils.RandomPassword(30, 0, 5)
		if err != nil {
			t.Fatalf("RandomPassword failed: %v", err)
		}
		hasSpecial := false
		for _, c := range pw {
			if strings.ContainsRune(specialChars, c) {
				hasSpecial = true
				break
			}
		}
		if !hasSpecial {
			t.Errorf("Expected password to contain special chars: %s", pw)
		}
	}
}

func TestRandomPasswordUniqueness(t *testing.T) {
	t.Parallel()
	pw1, err := utils.RandomPassword(32, 2, 2)
	if err != nil {
		t.Fatalf("RandomPassword failed: %v", err)
	}
	pw2, err := utils.RandomPassword(32, 2, 2)
	if err != nil {
		t.Fatalf("RandomPassword failed: %v", err)
	}
	if pw1 == pw2 {
		t.Error("Expected different random passwords")
	}
}
