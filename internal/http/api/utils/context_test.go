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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func makeTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestGetDB_Success(t *testing.T) {
	t.Parallel()
	c, _ := makeTestContext(t)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	c.Set("DB", database)

	db, ok := utils.GetDB(c)
	if !ok {
		t.Fatal("Expected GetDB to return true")
	}
	if db != database {
		t.Fatal("Expected GetDB to return the same database instance")
	}
}

func TestGetDB_WrongType(t *testing.T) {
	t.Parallel()
	c, w := makeTestContext(t)
	c.Set("DB", "not-a-db")

	db, ok := utils.GetDB(c)
	if ok {
		t.Fatal("Expected GetDB to return false for wrong type")
	}
	if db != nil {
		t.Fatal("Expected nil DB on failure")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", w.Code)
	}
}

func TestGetPaginatedDB_Success(t *testing.T) {
	t.Parallel()
	c, _ := makeTestContext(t)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	c.Set("PaginatedDB", database)

	db, ok := utils.GetPaginatedDB(c)
	if !ok {
		t.Fatal("Expected GetPaginatedDB to return true")
	}
	if db != database {
		t.Fatal("Expected GetPaginatedDB to return the same database instance")
	}
}

func TestGetPaginatedDB_WrongType(t *testing.T) {
	t.Parallel()
	c, w := makeTestContext(t)
	c.Set("PaginatedDB", 42)

	db, ok := utils.GetPaginatedDB(c)
	if ok {
		t.Fatal("Expected GetPaginatedDB to return false for wrong type")
	}
	if db != nil {
		t.Fatal("Expected nil DB on failure")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", w.Code)
	}
}

func TestGetConfig_Success(t *testing.T) {
	t.Parallel()
	c, _ := makeTestContext(t)
	cfg := &config.Config{NetworkName: "TestNet"}
	c.Set("Config", cfg)

	got, ok := utils.GetConfig(c)
	if !ok {
		t.Fatal("Expected GetConfig to return true")
	}
	if got.NetworkName != "TestNet" {
		t.Fatalf("Expected NetworkName 'TestNet', got '%s'", got.NetworkName)
	}
}

func TestGetConfig_WrongType(t *testing.T) {
	t.Parallel()
	c, w := makeTestContext(t)
	c.Set("Config", "not-a-config")

	cfg, ok := utils.GetConfig(c)
	if ok {
		t.Fatal("Expected GetConfig to return false for wrong type")
	}
	if cfg != nil {
		t.Fatal("Expected nil config on failure")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", w.Code)
	}
}

func TestGetReady_True(t *testing.T) {
	t.Parallel()
	c, _ := makeTestContext(t)
	ready := &atomic.Bool{}
	ready.Store(true)
	c.Set("Ready", ready)

	if !utils.GetReady(c) {
		t.Fatal("Expected GetReady to return true")
	}
}

func TestGetReady_False(t *testing.T) {
	t.Parallel()
	c, _ := makeTestContext(t)
	ready := &atomic.Bool{}
	c.Set("Ready", ready)

	if utils.GetReady(c) {
		t.Fatal("Expected GetReady to return false")
	}
}

func TestGetReady_WrongType(t *testing.T) {
	t.Parallel()
	c, _ := makeTestContext(t)
	c.Set("Ready", "not-a-bool")

	if utils.GetReady(c) {
		t.Fatal("Expected GetReady to return false for wrong type")
	}
}
