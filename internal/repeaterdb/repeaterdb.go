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

package repeaterdb

import (
	// Embed the repeaters.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmrdb"
	"github.com/puzpuzpuz/xsync/v4"
)

//go:embed repeaterdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/rptrs.json
//
//go:embed repeaters.json.xz
var comressedDMRRepeatersDB []byte

var repeaterDB = dmrdb.NewDB[DMRRepeater](dmrdb.Config[DMRRepeater]{ //nolint:gochecknoglobals
	CompressedData: comressedDMRRepeatersDB,
	BuiltInDateStr: builtInDateStr,
	Presize:        10000,
	EntityName:     "repeaters",
	Decode:         streamDecodeRepeaters,
})

var ErrDecodingDB = dmrdb.ErrDecodingDB

type DMRRepeater struct {
	Locator     uint   `json:"locator"`
	ID          uint   `json:"id"`
	Callsign    string `json:"callsign"`
	City        string `json:"city"`
	State       string `json:"state"`
	Country     string `json:"country"`
	Frequency   string `json:"frequency"`
	ColorCode   uint   `json:"color_code"`
	Offset      string `json:"offset"`
	Assigned    string `json:"assigned"`
	TSLinked    string `json:"ts_linked"`
	Trustee     string `json:"trustee"`
	MapInfo     string `json:"map_info"`
	Map         uint   `json:"map"`
	IPSCNetwork string `json:"ipsc_network"`
}

func IsValidRepeaterID(dmrID uint) bool {
	// Check that the repeater id is 6 digits
	if dmrID < 100000 || dmrID > 999999 {
		return false
	}
	return true
}

func ValidRepeaterCallsign(dmrID uint, callsign string) bool {
	repeater, ok := repeaterDB.Get(dmrID)
	if !ok {
		return false
	}

	if !strings.EqualFold(repeater.Trustee, callsign) {
		return false
	}

	return true
}

func streamDecodeRepeaters(dec *json.Decoder, m *xsync.Map[uint, DMRRepeater]) (int, error) {
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

		if key == "rptrs" {
			// Read opening [
			t, err = dec.Token()
			if err != nil {
				return 0, ErrDecodingDB
			}
			if delim, ok := t.(json.Delim); !ok || delim != '[' {
				return 0, ErrDecodingDB
			}

			for dec.More() {
				repeater, err := decodeRepeater(dec)
				if err != nil {
					return 0, ErrDecodingDB
				}
				m.Store(repeater.ID, repeater)
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

// setRepeaterField assigns a single JSON token value to the matching DMRRepeater field.
func setRepeaterField(r *DMRRepeater, key string, t json.Token) { //nolint:gocyclo
	switch key {
	case "locator":
		if f, ok := t.(float64); ok {
			r.Locator = uint(f) //nolint:gosec
		}
	case "id":
		if f, ok := t.(float64); ok {
			r.ID = uint(f) //nolint:gosec
		}
	case "callsign":
		if s, ok := t.(string); ok {
			r.Callsign = s
		}
	case "city":
		if s, ok := t.(string); ok {
			r.City = s
		}
	case "state":
		if s, ok := t.(string); ok {
			r.State = s
		}
	case "country":
		if s, ok := t.(string); ok {
			r.Country = s
		}
	case "frequency":
		if s, ok := t.(string); ok {
			r.Frequency = s
		}
	case "color_code":
		if f, ok := t.(float64); ok {
			r.ColorCode = uint(f) //nolint:gosec
		}
	case "offset":
		if s, ok := t.(string); ok {
			r.Offset = s
		}
	case "assigned":
		if s, ok := t.(string); ok {
			r.Assigned = s
		}
	case "ts_linked":
		if s, ok := t.(string); ok {
			r.TSLinked = s
		}
	case "trustee":
		if s, ok := t.(string); ok {
			r.Trustee = s
		}
	case "map_info":
		if s, ok := t.(string); ok {
			r.MapInfo = s
		}
	case "map":
		if f, ok := t.(float64); ok {
			r.Map = uint(f) //nolint:gosec
		}
	case "ipsc_network":
		if s, ok := t.(string); ok {
			r.IPSCNetwork = s
		}
	}
}

// decodeRepeater manually decodes a single DMRRepeater from the JSON token stream,
// avoiding the reflection overhead of json.Decoder.Decode.
func decodeRepeater(dec *json.Decoder) (DMRRepeater, error) {
	var r DMRRepeater

	// Read opening {
	t, err := dec.Token()
	if err != nil {
		return r, fmt.Errorf("%w: %w", ErrDecodingDB, err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return r, ErrDecodingDB
	}

	for dec.More() {
		t, err = dec.Token()
		if err != nil {
			return r, fmt.Errorf("%w: %w", ErrDecodingDB, err)
		}
		key, ok := t.(string)
		if !ok {
			return r, ErrDecodingDB
		}

		t, err = dec.Token()
		if err != nil {
			return r, fmt.Errorf("%w: %w", ErrDecodingDB, err)
		}

		setRepeaterField(&r, key, t)
	}

	// Read closing }
	if _, err = dec.Token(); err != nil {
		return r, fmt.Errorf("%w: %w", ErrDecodingDB, err)
	}

	return r, nil
}

func UnpackDB() error {
	if err := repeaterDB.UnpackDB(); err != nil {
		return fmt.Errorf("repeaterdb: %w", err)
	}
	return nil
}

func Len() int {
	return repeaterDB.Len()
}

func Get(id uint) (DMRRepeater, bool) {
	return repeaterDB.Get(id)
}

func Update(url string) error {
	if err := repeaterDB.Update(url); err != nil {
		return fmt.Errorf("repeaterdb: %w", err)
	}
	return nil
}

func GetDate() (time.Time, error) {
	t, err := repeaterDB.GetDate()
	if err != nil {
		return t, fmt.Errorf("repeaterdb: %w", err)
	}
	return t, nil
}
