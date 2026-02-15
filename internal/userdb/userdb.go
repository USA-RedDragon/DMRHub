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

package userdb

import (
	// Embed the users.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmrdb"
	"github.com/puzpuzpuz/xsync/v4"
)

//go:embed userdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/users.json
//
//go:embed users.json.xz
var compressedDMRUsersDB []byte

var userDB = dmrdb.NewDB[DMRUser](dmrdb.Config[DMRUser]{ //nolint:gochecknoglobals
	CompressedData: compressedDMRUsersDB,
	BuiltInDateStr: builtInDateStr,
	Presize:        250000,
	EntityName:     "users",
	Decode:         streamDecodeUsers,
})

var ErrDecodingDB = dmrdb.ErrDecodingDB

type DMRUser struct {
	ID       uint   `json:"id"`
	State    string `json:"state"`
	RadioID  uint   `json:"radio_id"`
	Surname  string `json:"surname"`
	City     string `json:"city"`
	Callsign string `json:"callsign"`
	Country  string `json:"country"`
	Name     string `json:"name"`
	FName    string `json:"fname"`
}

func IsValidUserID(dmrID uint) bool {
	// Check that the user id is 7 digits
	if dmrID < 1000000 || dmrID > 9999999 {
		return false
	}
	return true
}

func ValidUserCallsign(dmrID uint, callsign string) bool {
	user, ok := userDB.Get(dmrID)
	if !ok {
		return false
	}

	if user.ID != dmrID {
		return false
	}

	if !strings.EqualFold(user.Callsign, callsign) {
		return false
	}

	return true
}

func streamDecodeUsers(dec *json.Decoder, m *xsync.Map[uint, DMRUser]) (int, error) {
	// Read opening {
	t, err := dec.Token()
	if err != nil {
		return 0, ErrDecodingDB
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return 0, ErrDecodingDB
	}

	count := 0
	for dec.More() {
		t, err = dec.Token()
		if err != nil {
			return 0, ErrDecodingDB
		}
		key, ok := t.(string)
		if !ok {
			return 0, ErrDecodingDB
		}

		if key == "users" {
			// Read opening [
			t, err = dec.Token()
			if err != nil {
				return 0, ErrDecodingDB
			}
			if delim, ok := t.(json.Delim); !ok || delim != '[' {
				return 0, ErrDecodingDB
			}

			for dec.More() {
				user, err := decodeUser(dec)
				if err != nil {
					return 0, ErrDecodingDB
				}
				m.Store(user.ID, user)
				count++
			}

			// Read closing ]
			if _, err = dec.Token(); err != nil {
				return 0, ErrDecodingDB
			}
		} else {
			// Skip unknown key's value
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return 0, ErrDecodingDB
			}
		}
	}

	return count, nil
}

// decodeUser manually decodes a single DMRUser from the JSON token stream,
// avoiding the reflection overhead of json.Decoder.Decode.
func decodeUser(dec *json.Decoder) (DMRUser, error) {
	var user DMRUser

	// Read opening {
	t, err := dec.Token()
	if err != nil {
		return user, fmt.Errorf("%w: %w", ErrDecodingDB, err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return user, ErrDecodingDB
	}

	for dec.More() {
		t, err = dec.Token()
		if err != nil {
			return user, fmt.Errorf("%w: %w", ErrDecodingDB, err)
		}
		key, ok := t.(string)
		if !ok {
			return user, ErrDecodingDB
		}

		t, err = dec.Token()
		if err != nil {
			return user, fmt.Errorf("%w: %w", ErrDecodingDB, err)
		}

		switch key {
		case "id":
			if f, ok := t.(float64); ok {
				user.ID = uint(f) //nolint:gosec
			}
		case "radio_id":
			if f, ok := t.(float64); ok {
				user.RadioID = uint(f) //nolint:gosec
			}
		case "state":
			if s, ok := t.(string); ok {
				user.State = s
			}
		case "surname":
			if s, ok := t.(string); ok {
				user.Surname = s
			}
		case "city":
			if s, ok := t.(string); ok {
				user.City = s
			}
		case "callsign":
			if s, ok := t.(string); ok {
				user.Callsign = s
			}
		case "country":
			if s, ok := t.(string); ok {
				user.Country = s
			}
		case "name":
			if s, ok := t.(string); ok {
				user.Name = s
			}
		case "fname":
			if s, ok := t.(string); ok {
				user.FName = s
			}
		}
	}

	// Read closing }
	if _, err = dec.Token(); err != nil {
		return user, fmt.Errorf("%w: %w", ErrDecodingDB, err)
	}

	return user, nil
}

func UnpackDB() error {
	if err := userDB.UnpackDB(); err != nil {
		return fmt.Errorf("userdb: %w", err)
	}
	return nil
}

func Len() int {
	return userDB.Len()
}

func Get(dmrID uint) (DMRUser, bool) {
	return userDB.Get(dmrID)
}

func Update(url string) error {
	if err := userDB.Update(url); err != nil {
		return fmt.Errorf("userdb: %w", err)
	}
	return nil
}

func GetDate() (time.Time, error) {
	t, err := userDB.GetDate()
	if err != nil {
		return t, fmt.Errorf("userdb: %w", err)
	}
	return t, nil
}
