// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package hbrp_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/hbrp"
	"gorm.io/gorm"
)

func TestMakeServerInitialization(t *testing.T) {
	db := &gorm.DB{}
	redisClient := &servers.RedisClient{}
	callTracker := &calltracker.CallTracker{}
	version := "1.0.0"
	commit := "abc123"

	server := hbrp.MakeServer(db, nil, redisClient, callTracker, version, commit)

	if server.DB != db {
		t.Errorf("Expected DB to be %v, got %v", db, server.DB)
	}
	if server.Redis != redisClient {
		t.Errorf("Expected Redis to be %v, got %v", redisClient, server.Redis)
	}
	if server.CallTracker != callTracker {
		t.Errorf("Expected CallTracker to be %v, got %v", callTracker, server.CallTracker)
	}
	if server.Version != version {
		t.Errorf("Expected Version to be %s, got %s", version, server.Version)
	}
	if server.Commit != commit {
		t.Errorf("Expected Commit to be %s, got %s", commit, server.Commit)
	}
	if server.Started {
		t.Error("Expected Started to be false")
	}
}
