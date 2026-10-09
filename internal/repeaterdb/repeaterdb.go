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

var repeaterDB = newDB(comressedDMRRepeatersDB, builtInDateStr) //nolint:gochecknoglobals

// newDB creates a repeater database seeded with the given xz-compressed JSON dump.
func newDB(compressed []byte, dateStr string) *dmrdb.DB[DMRRepeater] {
	return dmrdb.NewDB[DMRRepeater](dmrdb.Config[DMRRepeater]{
		CompressedData: compressed,
		BuiltInDateStr: dateStr,
		Presize:        10000,
		EntityName:     "repeaters",
		Decode:         streamDecodeRepeaters,
	})
}

var ErrDecodingDB = dmrdb.ErrDecodingDB

type DMRRepeater struct {
	Locator     uint     `json:"locator"`
	ID          uint     `json:"id"`
	Callsign    string   `json:"callsign"`
	City        string   `json:"city"`
	State       string   `json:"state"`
	Country     string   `json:"country"`
	Frequency   string   `json:"frequency"`
	ColorCode   uint     `json:"color_code"`
	Offset      string   `json:"offset"`
	Assigned    string   `json:"assigned"`
	TSLinked    string   `json:"ts_linked"`
	Trustees    []string `json:"trustees"`
	MapInfo     string   `json:"map_info"`
	Map         uint     `json:"map"`
	IPSCNetwork string   `json:"ipsc_network"`
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

	for _, trustee := range repeater.Trustees {
		if strings.EqualFold(trustee, callsign) {
			return true
		}
	}

	return false
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
					return 0, err
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

// setRepeaterField decodes the value of a single repeater field, whose first token is t.
func setRepeaterField(dec *json.Decoder, r *DMRRepeater, key string, t json.Token) error {
	var err error
	switch key {
	case "locator":
		err = dmrdb.SetUint(&r.Locator, t)
	case "id":
		err = dmrdb.SetUint(&r.ID, t)
	case "callsign":
		err = dmrdb.SetString(&r.Callsign, t)
	case "city":
		err = dmrdb.SetString(&r.City, t)
	case "state":
		err = dmrdb.SetString(&r.State, t)
	case "country":
		err = dmrdb.SetString(&r.Country, t)
	case "frequency":
		err = dmrdb.SetString(&r.Frequency, t)
	case "color_code":
		err = dmrdb.SetUint(&r.ColorCode, t)
	case "offset":
		err = dmrdb.SetString(&r.Offset, t)
	case "assigned":
		err = dmrdb.SetString(&r.Assigned, t)
	case "ts_linked":
		err = dmrdb.SetString(&r.TSLinked, t)
	case "map_info":
		err = dmrdb.SetString(&r.MapInfo, t)
	case "map":
		err = dmrdb.SetUint(&r.Map, t)
	case "ipsc_network":
		err = dmrdb.SetString(&r.IPSCNetwork, t)
	case "trustee":
		r.Trustees, err = decodeTrustees(dec, t)
	default:
		err = dmrdb.SkipValue(dec, t)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
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

		if err := setRepeaterField(dec, &r, key, t); err != nil {
			return r, err
		}
	}

	// Read closing }
	if _, err = dec.Token(); err != nil {
		return r, fmt.Errorf("%w: %w", ErrDecodingDB, err)
	}

	if r.ID == 0 {
		return r, fmt.Errorf("%w: repeater without an id", ErrDecodingDB)
	}

	return r, nil
}

// decodeTrustees reads the trustee value, which RadioID.net now publishes as an
// array of callsigns but older dumps carry as a single string.
func decodeTrustees(dec *json.Decoder, t json.Token) ([]string, error) {
	switch v := t.(type) {
	case string:
		return []string{v}, nil
	case nil:
		return nil, nil
	case json.Delim:
		if v != '[' {
			return nil, ErrDecodingDB
		}
		var trustees []string
		for dec.More() {
			t, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrDecodingDB, err)
			}
			switch s := t.(type) {
			case string:
				trustees = append(trustees, s)
			case nil:
			default:
				return nil, fmt.Errorf("%w: expected a callsign, got %T", ErrDecodingDB, t)
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecodingDB, err)
		}
		return trustees, nil
	default:
		return nil, ErrDecodingDB
	}
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
