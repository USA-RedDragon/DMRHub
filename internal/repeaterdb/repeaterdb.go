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
	"fmt"
	"log"
	"math"
	"path"

	// Embed the repeaters.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/glebarez/sqlite"
	"github.com/ulikunitz/xz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
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
	db *gorm.DB

	builtInDate time.Time
	isInited    atomic.Bool
	isDone      atomic.Bool
}

type LastUpdate struct {
	LastUpdate time.Time
}

type dmrRepeaterDB struct {
	Repeaters []DMRRepeater `json:"rptrs"`
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

	var repeater DMRRepeater
	repeaterDB.db.Find(&repeater, "id = ?", dmrID)

	if repeater.ID != fmt.Sprint(dmrID) {
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

func setDB() {
	fileName := path.Join(config.GetConfig().DMRDatabaseDirectory, "repeaters.sqlite")
	db, err := gorm.Open(sqlite.Open(fileName), &gorm.Config{})
	if err != nil {
		logging.Errorf("Could not open database: %s", err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}

	err = db.AutoMigrate(&DMRRepeater{}, &LastUpdate{})
	if err != nil {
		logging.Errorf("Could not migrate database: %s", err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}

	repeaterDB.db = db
}

func UnpackDB() {
	lastInit := repeaterDB.isInited.Swap(true)
	if !lastInit {
		setDB()
		var err error
		repeaterDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			logging.Errorf("Error parsing built-in date: %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		lastUpdate := LastUpdate{}
		tx := repeaterDB.db.First(&lastUpdate)
		if tx.RowsAffected > 0 {
			if !lastUpdate.LastUpdate.IsZero() && (lastUpdate.LastUpdate.After(repeaterDB.builtInDate) || lastUpdate.LastUpdate.Equal(repeaterDB.builtInDate)) {
				logging.Error("Repeater DB loaded")
				repeaterDB.isDone.Store(true)
				return
			} else {
				logging.Errorf("Last update %v is before built-in date %v", lastUpdate.LastUpdate, repeaterDB.builtInDate)
			}
		} else {
			logging.Errorf("Error getting last update: %v", tx.Error)
		}
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			logging.Errorf("NewReader error %s", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		uncompressedJSON, err := io.ReadAll(dbReader)
		if err != nil {
			logging.Errorf("ReadAll error %s", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		var tmpDB dmrRepeaterDB
		if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
			logging.Errorf("Error decoding DMR repeaters database: %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}

		repeaterDB.db.FirstOrCreate(&LastUpdate{LastUpdate: repeaterDB.builtInDate})
		err = bulkUpsert(&tmpDB)
		if err != nil {
			logging.Errorf("Error bulk inserting repeaters: %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		logging.Error("Repeater DB loaded")
		repeaterDB.isDone.Store(true)
	}

	for !repeaterDB.isDone.Load() {
		time.Sleep(waitTime)
	}
}

func Len() int {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	var count int64
	repeaterDB.db.Model(&DMRRepeater{}).Count(&count)
	if repeaterDB.db.Error != nil {
		logging.Errorf("Error counting repeaters: %v", repeaterDB.db.Error)
		return 0
	}
	return int(count)
}

func Get(id uint) (DMRRepeater, bool) {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	var repeater DMRRepeater
	repeaterDB.db.Find(&repeater, "id = ?", fmt.Sprint(id))
	if repeater.ID != fmt.Sprint(id) {
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

	uncompressedJSON, err := io.ReadAll(resp.Body)
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
	if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
		logging.Errorf("Error decoding DMR repeaters database: %v", err)
		return ErrUpdateFailed
	}

	if len(tmpDB.Repeaters) == 0 {
		logging.Error("No DMR repeaters found in database")
		return ErrUpdateFailed
	}

	repeaterDB.db.FirstOrCreate(&LastUpdate{LastUpdate: time.Now()})
	err = bulkUpsert(&tmpDB)
	if err != nil {
		logging.Errorf("Error bulk inserting repeaters: %v", err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}

	logging.Errorf("Update complete. Loaded %d DMR repeaters", Len())

	return nil
}

func bulkUpsert(tmpDB *dmrRepeaterDB) error {
	// SQLITE_LIMIT_VARIABLE_NUMBER
	const limit = 32766
	// Break the repeaters into batches of SQLITE_LIMIT_VARIABLE_NUMBER / 15 (15 is the number of columns in the table)
	// This is to prevent SQLITE_ERROR: too many SQL variables
	batchSize := math.Floor(float64(limit) / 15)
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold: time.Second * 5, // Slow SQL threshold
			LogLevel:      logger.Warn,     // Log level
		},
	)
	tx := repeaterDB.db.Session(&gorm.Session{Logger: newLogger})
	for i := 0; i < len(tmpDB.Repeaters); i += int(batchSize) {
		end := i + int(batchSize)
		if end > len(tmpDB.Repeaters) {
			end = len(tmpDB.Repeaters)
		}
		tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"locator", "callsign", "city", "state", "country", "frequency", "color_code", "offset", "assigned", "ts_linked", "trustee", "map_info", "map", "ip_sc_network"}),
		}).CreateInBatches(tmpDB.Repeaters[i:end], len(tmpDB.Repeaters[i:end]))
		if tx.Error != nil {
			return tx.Error
		}
	}
	return nil
}

func GetDate() time.Time {
	if !repeaterDB.isDone.Load() {
		UnpackDB()
	}
	var lastUpdate LastUpdate
	repeaterDB.db.Find(&lastUpdate)
	if lastUpdate.LastUpdate.IsZero() {
		logging.Error("Error loading DMR repeaters database")
		return repeaterDB.builtInDate
	}
	return lastUpdate.LastUpdate
}
