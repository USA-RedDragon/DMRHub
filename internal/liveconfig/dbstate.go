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

package liveconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// DatabaseStateFile is the file, in the working directory of DMRHub,
// where the setup wizard keeps a database setting that it fixed. It cannot
// keep it in the database that DMRHub could not reach. The default SQLite
// database lives in the same directory.
const DatabaseStateFile = "dmrhub-database.json"

// ErrDatabaseLocked is a change in the setup wizard of the database
// settings, which the deployment sets.
var ErrDatabaseLocked = errors.New("the deployment sets the database, in the config file, the environment or a flag; fix it there and restart DMRHub")

// DatabaseState is the database setting that the setup wizard keeps in
// Path. DMRHub uses it only when the config file, the environment and the
// flags set no database setting.
type DatabaseState struct {
	Path string
}

// DatabaseStateIn returns the DatabaseState of the working directory.
func DatabaseStateIn() DatabaseState {
	path, err := filepath.Abs(DatabaseStateFile)
	if err != nil {
		path = DatabaseStateFile
	}
	return DatabaseState{Path: path}
}

// DatabaseLocked reports whether the config file, the environment or a
// flag sets any database setting.
func DatabaseLocked(locks Locks) bool {
	for p := range locks {
		if under(p, "database") {
			return true
		}
	}
	return false
}

// Load returns the database setting of the file, and whether there is one.
func (s DatabaseState) Load() (config.Database, bool, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return config.Database{}, false, nil
	}
	if err != nil {
		return config.Database{}, false, fmt.Errorf("failed to read %s: %w", s.Path, err)
	}
	var st databaseFile
	if err := json.Unmarshal(b, &st); err != nil {
		return config.Database{}, false, fmt.Errorf("%s is not a database setting: %w", s.Path, err)
	}
	d := st.Database
	d.Password = st.Password
	return d, true, nil
}

type databaseFile struct {
	Database config.Database `json:"database"`
	Password string          `json:"password"`
}

// Save writes d to the file, readable by its owner only. It writes a new
// file and renames it over the old one, so that a crash leaves the old one
// whole.
func (s DatabaseState) Save(d config.Database) error {
	b, err := json.MarshalIndent(databaseFile{Database: d, Password: d.Password}, "", "  ") //nolint:gosec // the state file must keep the database password; Save makes it readable by its owner only
	if err != nil {
		return fmt.Errorf("failed to encode the database setting: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".dmrhub-database-*")
	if err != nil {
		return fmt.Errorf("cannot write in %s: %w", filepath.Dir(s.Path), err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to protect %s: %w", name, err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	if err := os.Rename(name, s.Path); err != nil {
		return fmt.Errorf("failed to write %s: %w", s.Path, err)
	}
	return nil
}

// Writable returns why the setup wizard cannot keep a database setting in
// the file, or nil when it can.
func (s DatabaseState) Writable() error {
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".dmrhub-database-*")
	if err != nil {
		return fmt.Errorf("DMRHub cannot write in %s: %w", filepath.Dir(s.Path), err)
	}
	name := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(name)
	return nil
}
