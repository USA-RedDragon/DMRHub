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

package testutils

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
)

//go:embed radioid/users.json
var fictionalUsers []byte

//go:embed radioid/repeaters.json
var fictionalRepeaters []byte

// UseFictionalRadioID replaces the RadioID user and repeater data of the
// process with fictional entries, so tests check DMR IDs and callsigns that
// belong to no real person. Call it from TestMain, before any test runs. It
// exits the process when it cannot load them.
func UseFictionalRadioID() {
	if err := useFictionalRadioID(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func useFictionalRadioID() error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users.json":
			_, _ = w.Write(fictionalUsers)
		case "/repeaters.json":
			_, _ = w.Write(fictionalRepeaters)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := userdb.Update(srv.URL + "/users.json"); err != nil {
		return fmt.Errorf("could not load the fictional users: %w", err)
	}
	if err := repeaterdb.Update(srv.URL + "/repeaters.json"); err != nil {
		return fmt.Errorf("could not load the fictional repeaters: %w", err)
	}
	return nil
}
