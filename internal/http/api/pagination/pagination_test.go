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

package pagination_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/pagination"
)

func TestNewPaginate(t *testing.T) {
	t.Parallel()
	p := pagination.NewPaginate(10, 1)
	if p == nil {
		t.Fatal("Expected non-nil Paginate")
	}
}

func TestPaginateFirstPage(t *testing.T) {
	t.Parallel()
	p := pagination.NewPaginate(10, 1)
	if p == nil {
		t.Fatal("Expected non-nil Paginate")
	}
	// Cannot easily test the GORM scope directly without a DB, but we verify the struct creates properly
}

func TestPaginateSecondPage(t *testing.T) {
	t.Parallel()
	p := pagination.NewPaginate(10, 2)
	if p == nil {
		t.Fatal("Expected non-nil Paginate")
	}
}

func TestPaginateZeroLimit(t *testing.T) {
	t.Parallel()
	p := pagination.NewPaginate(0, 1)
	if p == nil {
		t.Fatal("Expected non-nil Paginate")
	}
}

func TestPaginateLargePageNumber(t *testing.T) {
	t.Parallel()
	p := pagination.NewPaginate(25, 100)
	if p == nil {
		t.Fatal("Expected non-nil Paginate")
	}
}
