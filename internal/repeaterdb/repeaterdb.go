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
	"bytes"
	"context"
	// Embed the repeaters.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/ulikunitz/xz"
)

//go:embed repeaterdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/rptrs.json
//
//go:embed repeaters.json.xz
var comressedDMRRepeatersDB []byte

var repeaterDB RepeaterDB //nolint:gochecknoglobals

var (
	ErrUpdateFailed = errors.New("update failed")
	ErrUnmarshal    = errors.New("unmarshal failed")
	ErrLoading      = errors.New("error loading DMR users database")
	ErrNoRepeaters  = errors.New("no DMR repeaters found in database")
	ErrParsingDate  = errors.New("error parsing built-in date")
	ErrXZReader     = errors.New("error creating xz reader")
	ErrReadDB       = errors.New("error reading database")
	ErrDecodingDB   = errors.New("error decoding DMR repeaters database")
)

const waitTime = 100 * time.Millisecond

type dbMetadata struct {
	Count int
	Date  time.Time
}

type RepeaterDB struct {
	metadata               atomic.Value // stores dbMetadata
	dmrRepeaterMap         *xsync.Map[uint, DMRRepeater]
	dmrRepeaterMapUpdating *xsync.Map[uint, DMRRepeater]

	builtInDate time.Time
	isInited    atomic.Bool
	isDone      atomic.Bool
}

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
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return false
		}
	}

	repeater, ok := repeaterDB.dmrRepeaterMap.Load(dmrID)
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
	lastInit := repeaterDB.isInited.Swap(true)
	if !lastInit {
		repeaterDB.dmrRepeaterMapUpdating = xsync.NewMap[uint, DMRRepeater](xsync.WithPresize(10000), xsync.WithGrowOnly())
		var err error
		repeaterDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			return ErrParsingDate
		}
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			return ErrXZReader
		}

		count, err := streamDecodeRepeaters(json.NewDecoder(dbReader), repeaterDB.dmrRepeaterMapUpdating)
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrNoRepeaters
		}

		repeaterDB.metadata.Store(dbMetadata{Count: count, Date: repeaterDB.builtInDate})
		repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
		repeaterDB.dmrRepeaterMapUpdating = xsync.NewMap[uint, DMRRepeater]()
		repeaterDB.isDone.Store(true)
	}

	for !repeaterDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	meta, ok := repeaterDB.metadata.Load().(dbMetadata)
	if !ok {
		return ErrLoading
	}
	if meta.Count == 0 {
		return ErrNoRepeaters
	}
	return nil
}

func Len() int {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return 0
		}
	}
	meta, ok := repeaterDB.metadata.Load().(dbMetadata)
	if !ok {
		slog.Error("Error loading DMR repeaters database")
		return 0
	}
	return meta.Count
}

func Get(id uint) (DMRRepeater, bool) {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return DMRRepeater{}, false
		}
	}
	repeater, ok := repeaterDB.dmrRepeaterMap.Load(id)
	if !ok {
		return DMRRepeater{}, false
	}
	return repeater, true
}

func Update(url string) error {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return ErrUpdateFailed
		}
	}
	const updateTimeout = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return ErrUpdateFailed
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ErrUpdateFailed
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			slog.Error("Error closing response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return ErrUpdateFailed
	}

	repeaterDB.dmrRepeaterMapUpdating = xsync.NewMap[uint, DMRRepeater](xsync.WithPresize(Len()), xsync.WithGrowOnly())
	count, err := streamDecodeRepeaters(json.NewDecoder(resp.Body), repeaterDB.dmrRepeaterMapUpdating)
	if err != nil {
		slog.Error("Error decoding DMR repeaters database", "error", err)
		return ErrUpdateFailed
	}

	if count == 0 {
		slog.Error("No DMR repeaters found in database")
		return ErrUpdateFailed
	}

	repeaterDB.metadata.Store(dbMetadata{Count: count, Date: time.Now()})
	repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
	repeaterDB.dmrRepeaterMapUpdating = xsync.NewMap[uint, DMRRepeater]()

	slog.Info("Update complete", "loadedRepeaters", Len())

	return nil
}

func GetDate() (time.Time, error) {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return time.Time{}, err
		}
	}
	meta, ok := repeaterDB.metadata.Load().(dbMetadata)
	if !ok {
		slog.Error("Error loading DMR repeaters database")
		return time.Time{}, ErrLoading
	}
	return meta.Date, nil
}
