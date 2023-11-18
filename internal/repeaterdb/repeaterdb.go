// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/puzpuzpuz/xsync/v2"
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
	Locator     string `json:"locator"`
	ID          string `json:"id"`
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
		UnpackDB()
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

func UnpackDB() {
	lastInit := repeaterDB.isInited.Swap(true)
	if !lastInit {
		repeaterDB.dmrRepeaterMap = xsync.NewIntegerMapOf[uint, DMRRepeater]()
		repeaterDB.dmrRepeaterMapUpdating = xsync.NewIntegerMapOf[uint, DMRRepeater]()
		var err error
		repeaterDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			logging.Errorf("Error parsing built-in date: %v", err)
			os.Exit(1)
		}
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			logging.Errorf("NewReader error %s", err)
			os.Exit(1)
		}
		repeaterDB.uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			logging.Errorf("ReadAll error %s", err)
			os.Exit(1)
		}
		var tmpDB dmrRepeaterDB
		if err := json.Unmarshal(repeaterDB.uncompressedJSON, &tmpDB); err != nil {
			logging.Errorf("Error decoding DMR repeaters database: %v", err)
			os.Exit(1)
		}

		tmpDB.Date = repeaterDB.builtInDate
		repeaterDB.dmrRepeaters.Store(tmpDB)
		for i := range tmpDB.Repeaters {
			id, err := strconv.Atoi(tmpDB.Repeaters[i].ID)
			if err != nil {
				continue
			}
			repeaterDB.dmrRepeaterMapUpdating.Store(uint(id), tmpDB.Repeaters[i])
		}

		repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
		repeaterDB.dmrRepeaterMapUpdating = xsync.NewIntegerMapOf[uint, DMRRepeater]()
		repeaterDB.isDone.Store(true)
	}

	for !repeaterDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	rptdb, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		logging.Errorf("Error loading DMR users database")
		os.Exit(1)
	}
	if len(rptdb.Repeaters) == 0 {
		logging.Errorf("No DMR users found in database")
		os.Exit(1)
	}
}

func Len() int {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	db, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		logging.Error("Error loading DMR users database")
	}
	return len(db.Repeaters)
}

func Get(id uint) (DMRRepeater, bool) {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	repeater, ok := repeaterDB.dmrRepeaterMap.Load(id)
	if !ok {
		return DMRRepeater{}, false
	}
	return repeater, true
}

func Update() error {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
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

	repeaterDB.dmrRepeaterMapUpdating = xsync.NewIntegerMapOf[uint, DMRRepeater]()
	for i := range tmpDB.Repeaters {
		id, err := strconv.Atoi(tmpDB.Repeaters[i].ID)
		if err != nil {
			continue
		}
		repeaterDB.dmrRepeaterMapUpdating.Store(uint(id), tmpDB.Repeaters[i])
	}

	repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
	repeaterDB.dmrRepeaterMapUpdating = xsync.NewIntegerMapOf[uint, DMRRepeater]()

	logging.Errorf("Update complete. Loaded %d DMR repeaters", Len())

	return nil
}

func GetDate() time.Time {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	db, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		logging.Error("Error loading DMR users database")
	}
	return db.Date
}
