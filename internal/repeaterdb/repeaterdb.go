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

package repeaterdb

import (
	"bytes"
	"context"
	// Embed the repeaters.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/ulikunitz/xz"
)

//go:embed repeaterdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/rptrs.json
//
//go:embed repeaters.json.xz
var comressedDMRRepeatersDB []byte

var repeaterDB RepeaterDB //nolint:golint,gochecknoglobals

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

type RepeaterDB struct {
	uncompressedJSON       []byte
	dmrRepeaters           atomic.Value
	dmrRepeaterMap         *xsync.MapOf[uint, DMRRepeater]
	dmrRepeaterMapUpdating *xsync.MapOf[uint, DMRRepeater]

	builtInDate time.Time
	isInited    atomic.Bool
	isDone      atomic.Bool
}

type dmrRepeaterDB struct {
	Repeaters []DMRRepeater `json:"rptrs"`
	Date      time.Time     `json:"-"`
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
			logging.Errorf("Error unpacking database: %v", err)
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

func (e *dmrRepeaterDB) Unmarshal(b []byte) error {
	err := json.Unmarshal(b, e)
	if err != nil {
		return ErrUnmarshal
	}
	return nil
}

func UnpackDB() error {
	lastInit := repeaterDB.isInited.Swap(true)
	if !lastInit {
		repeaterDB.dmrRepeaterMap = xsync.NewMapOf[uint, DMRRepeater]()
		repeaterDB.dmrRepeaterMapUpdating = xsync.NewMapOf[uint, DMRRepeater]()
		var err error
		repeaterDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			return ErrParsingDate
		}
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			return ErrXZReader
		}
		repeaterDB.uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			return ErrReadDB
		}
		var tmpDB dmrRepeaterDB
		if err := json.Unmarshal(repeaterDB.uncompressedJSON, &tmpDB); err != nil {
			return ErrDecodingDB
		}

		tmpDB.Date = repeaterDB.builtInDate
		repeaterDB.dmrRepeaters.Store(tmpDB)
		for i := range tmpDB.Repeaters {
			repeaterDB.dmrRepeaterMapUpdating.Store(tmpDB.Repeaters[i].ID, tmpDB.Repeaters[i])
		}

		repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
		repeaterDB.dmrRepeaterMapUpdating = xsync.NewMapOf[uint, DMRRepeater]()
		repeaterDB.isDone.Store(true)
	}

	for !repeaterDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	rptdb, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		return ErrLoading
	}
	if len(rptdb.Repeaters) == 0 {
		return ErrNoRepeaters
	}
	return nil
}

func Len() int {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return 0
		}
	}
	db, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		logging.Error("Error loading DMR users database")
	}
	return len(db.Repeaters)
}

func Get(id uint) (DMRRepeater, bool) {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return DMRRepeater{}, false
		}
	}
	repeater, ok := repeaterDB.dmrRepeaterMap.Load(id)
	if !ok {
		return DMRRepeater{}, false
	}
	return repeater, true
}

func Update() error {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return ErrUpdateFailed
		}
	}
	const updateTimeout = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.radioid.net/static/rptrs.json", nil)
	if err != nil {
		return ErrUpdateFailed
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ErrUpdateFailed
	}

	if resp.StatusCode != http.StatusOK {
		return ErrUpdateFailed
	}

	repeaterDB.uncompressedJSON, err = io.ReadAll(resp.Body)
	if err != nil {
		return ErrUpdateFailed
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			logging.Errorf("Error closing response body: %v", err)
		}
	}()
	var tmpDB dmrRepeaterDB
	if err := json.Unmarshal(repeaterDB.uncompressedJSON, &tmpDB); err != nil {
		logging.Errorf("Error decoding DMR repeaters database: %v", err)
		return ErrUpdateFailed
	}

	if len(tmpDB.Repeaters) == 0 {
		logging.Error("No DMR repeaters found in database")
		return ErrUpdateFailed
	}

	tmpDB.Date = time.Now()
	repeaterDB.dmrRepeaters.Store(tmpDB)

	repeaterDB.dmrRepeaterMapUpdating = xsync.NewMapOf[uint, DMRRepeater]()
	for i := range tmpDB.Repeaters {
		repeaterDB.dmrRepeaterMapUpdating.Store(tmpDB.Repeaters[i].ID, tmpDB.Repeaters[i])
	}

	repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
	repeaterDB.dmrRepeaterMapUpdating = xsync.NewMapOf[uint, DMRRepeater]()

	logging.Errorf("Update complete. Loaded %d DMR repeaters", Len())

	return nil
}

func GetDate() (time.Time, error) {
	if !repeaterDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return time.Time{}, err
		}
	}
	db, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		logging.Error("Error loading DMR users database")
	}
	return db.Date, nil
}
