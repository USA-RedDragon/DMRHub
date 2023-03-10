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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
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
	uncompressedJSON           []byte
	dmrRepeaters               atomic.Value
	dmrRepeaterMap             map[uint]DMRRepeater
	dmrRepeaterMapLock         sync.RWMutex
	dmrRepeaterMapUpdating     map[uint]DMRRepeater
	dmrRepeaterMapUpdatingLock sync.RWMutex

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
	repeaterDB.dmrRepeaterMapLock.RLock()
	repeater, ok := repeaterDB.dmrRepeaterMap[dmrID]
	repeaterDB.dmrRepeaterMapLock.RUnlock()
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
		repeaterDB.dmrRepeaterMapLock.Lock()
		repeaterDB.dmrRepeaterMap = make(map[uint]DMRRepeater)
		repeaterDB.dmrRepeaterMapLock.Unlock()
		repeaterDB.dmrRepeaterMapUpdatingLock.Lock()
		repeaterDB.dmrRepeaterMapUpdating = make(map[uint]DMRRepeater)
		repeaterDB.dmrRepeaterMapUpdatingLock.Unlock()
		var err error
		repeaterDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			klog.Fatalf("Error parsing built-in date: %v", err)
		}
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		repeaterDB.uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		var tmpDB dmrRepeaterDB
		if err := json.Unmarshal(repeaterDB.uncompressedJSON, &tmpDB); err != nil {
			klog.Exitf("Error decoding DMR repeaters database: %v", err)
		}

		tmpDB.Date = repeaterDB.builtInDate
		repeaterDB.dmrRepeaters.Store(tmpDB)
		repeaterDB.dmrRepeaterMapUpdatingLock.Lock()
		for i := range tmpDB.Repeaters {
			id, err := strconv.Atoi(tmpDB.Repeaters[i].ID)
			if err != nil {
				klog.Errorf("Error converting repeater ID to int: %v", err)
				continue
			}
			repeaterDB.dmrRepeaterMapUpdating[uint(id)] = tmpDB.Repeaters[i]
		}
		repeaterDB.dmrRepeaterMapUpdatingLock.Unlock()

		repeaterDB.dmrRepeaterMapLock.Lock()
		repeaterDB.dmrRepeaterMapUpdatingLock.RLock()
		repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
		repeaterDB.dmrRepeaterMapUpdatingLock.RUnlock()
		repeaterDB.dmrRepeaterMapLock.Unlock()
		repeaterDB.isDone.Store(true)
	}

	for !repeaterDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	rptdb, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		klog.Exit("Error loading DMR users database")
	}
	if len(rptdb.Repeaters) == 0 {
		klog.Exit("No DMR users found in database")
	}
}

func Len() int {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	db, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		klog.Error("Error loading DMR users database")
	}
	return len(db.Repeaters)
}

func Get(id uint) (DMRRepeater, bool) {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	repeaterDB.dmrRepeaterMapLock.RLock()
	user, ok := repeaterDB.dmrRepeaterMap[id]
	repeaterDB.dmrRepeaterMapLock.RUnlock()
	return user, ok
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
			klog.Errorf("Error closing response body: %v", err)
		}
	}()
	var tmpDB dmrRepeaterDB
	if err := json.Unmarshal(repeaterDB.uncompressedJSON, &tmpDB); err != nil {
		klog.Errorf("Error decoding DMR repeaters database: %v", err)
		return ErrUpdateFailed
	}

	if len(tmpDB.Repeaters) == 0 {
		klog.Exit("No DMR repeaters found in database")
	}

	tmpDB.Date = time.Now()
	repeaterDB.dmrRepeaters.Store(tmpDB)

	repeaterDB.dmrRepeaterMapUpdatingLock.Lock()
	repeaterDB.dmrRepeaterMapUpdating = make(map[uint]DMRRepeater)
	for i := range tmpDB.Repeaters {
		id, err := strconv.Atoi(tmpDB.Repeaters[i].ID)
		if err != nil {
			klog.Errorf("Error converting repeater ID to int: %v", err)
			continue
		}
		repeaterDB.dmrRepeaterMapUpdating[uint(id)] = tmpDB.Repeaters[i]
	}
	repeaterDB.dmrRepeaterMapUpdatingLock.Unlock()

	repeaterDB.dmrRepeaterMapLock.Lock()
	repeaterDB.dmrRepeaterMapUpdatingLock.RLock()
	repeaterDB.dmrRepeaterMap = repeaterDB.dmrRepeaterMapUpdating
	repeaterDB.dmrRepeaterMapUpdatingLock.RUnlock()
	repeaterDB.dmrRepeaterMapLock.Unlock()

	logging.GetLogger(logging.Error).Logf(Update, "Update complete. Loaded %d DMR repeaters", Len())

	return nil
}

func GetDate() time.Time {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	db, ok := repeaterDB.dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		klog.Error("Error loading DMR users database")
	}
	return db.Date
}
