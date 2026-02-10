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

package apimodels_test

import (
	"encoding/json"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
)

func TestAuthLoginMarshalJSON(t *testing.T) {
	t.Parallel()
	login := apimodels.AuthLogin{
		Username: "testuser",
		Callsign: "KI5VMF",
		Password: "secret",
	}
	data, err := json.Marshal(login)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if result["username"] != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", result["username"])
	}
	if result["callsign"] != "KI5VMF" {
		t.Errorf("Expected callsign 'KI5VMF', got '%s'", result["callsign"])
	}
	if result["password"] != "secret" {
		t.Errorf("Expected password 'secret', got '%s'", result["password"])
	}
}

func TestAuthLoginUnmarshalJSON(t *testing.T) {
	t.Parallel()
	data := `{"username":"user1","callsign":"AB1CD","password":"pass123"}`
	var login apimodels.AuthLogin
	if err := json.Unmarshal([]byte(data), &login); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if login.Username != "user1" {
		t.Errorf("Expected 'user1', got '%s'", login.Username)
	}
	if login.Callsign != "AB1CD" {
		t.Errorf("Expected 'AB1CD', got '%s'", login.Callsign)
	}
	if login.Password != "pass123" {
		t.Errorf("Expected 'pass123', got '%s'", login.Password)
	}
}

func TestAuthLoginEmptyFields(t *testing.T) {
	t.Parallel()
	data := `{}`
	var login apimodels.AuthLogin
	if err := json.Unmarshal([]byte(data), &login); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if login.Username != "" {
		t.Errorf("Expected empty username, got '%s'", login.Username)
	}
	if login.Callsign != "" {
		t.Errorf("Expected empty callsign, got '%s'", login.Callsign)
	}
	if login.Password != "" {
		t.Errorf("Expected empty password, got '%s'", login.Password)
	}
}
