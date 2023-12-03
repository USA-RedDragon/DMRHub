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

package userdb

import (
	"bytes"
	"context"
	"log"
	"math"
	"path"

	// Embed the users.json.xz file into the binary.
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

//go:embed userdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/users.json
//
//go:embed users.json.xz
var compressedDMRUsersDB []byte

var userDB UserDB //nolint:golint,gochecknoglobals

var (
	ErrUpdateFailed = errors.New("update failed")
	ErrUnmarshal    = errors.New("unmarshal failed")
)

const waitTime = 100 * time.Millisecond

type UserDB struct {
	db *gorm.DB

	builtInDate time.Time
	isInited    atomic.Bool
	isDone      atomic.Bool
}

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

type LastUpdate struct {
	LastUpdate time.Time
}

type dmrUserDB struct {
	Users []DMRUser `json:"users"`
}

func IsValidUserID(dmrID uint) bool {
	// Check that the user id is 7 digits
	if dmrID < 1000000 || dmrID > 9999999 {
		return false
	}
	return true
}

func ValidUserCallsign(dmrID uint, callsign string) bool {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	var user DMRUser
	userDB.db.Find(&user, "id = ?", dmrID)
	if user.ID != dmrID {
		return false
	}

	if !strings.EqualFold(user.Callsign, callsign) {
		return false
	}

	return true
}

func (e *dmrUserDB) Unmarshal(b []byte) error {
	err := json.Unmarshal(b, e)
	if err != nil {
		return ErrUnmarshal
	}
	return nil
}

func setDB() {
	fileName := path.Join(config.GetConfig().DMRDatabaseDirectory, "users.sqlite")
	db, err := gorm.Open(sqlite.Open(fileName), &gorm.Config{})
	if err != nil {
		logging.Errorf("Could not open database: %s", err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}

	err = db.AutoMigrate(&DMRUser{}, &LastUpdate{})
	if err != nil {
		logging.Errorf("Could not migrate database: %s", err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}

	userDB.db = db
}

func UnpackDB() {
	lastInit := userDB.isInited.Swap(true)
	if !lastInit {
		setDB()
		var err error
		userDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			logging.Errorf("Error parsing built-in date: %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		lastUpdate := LastUpdate{}
		tx := userDB.db.First(&lastUpdate)
		if tx.RowsAffected > 0 {
			if !lastUpdate.LastUpdate.IsZero() && (lastUpdate.LastUpdate.After(userDB.builtInDate) || lastUpdate.LastUpdate.Equal(userDB.builtInDate)) {
				logging.Error("User DB loaded")
				userDB.isDone.Store(true)
				return
			} else {
				logging.Errorf("Last update %v is before built-in date %v", lastUpdate.LastUpdate, userDB.builtInDate)
			}
		} else {
			logging.Errorf("Error getting last update: %v", tx.Error)
		}
		dbReader, err := xz.NewReader(bytes.NewReader(compressedDMRUsersDB))
		if err != nil {
			logging.Errorf("NewReader error %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		uncompressedJSON, err := io.ReadAll(dbReader)
		if err != nil {
			logging.Errorf("ReadAll error %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		var tmpDB dmrUserDB
		if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
			logging.Errorf("Error decoding DMR users database: %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}
		userDB.db.FirstOrCreate(&LastUpdate{LastUpdate: userDB.builtInDate})
		err = bulkUpsert(&tmpDB)
		if err != nil {
			logging.Errorf("Error bulk inserting users: %v", err)
			time.Sleep(10 * time.Second)
			os.Exit(1)
		}

		logging.Error("User DB loaded")

		userDB.isDone.Store(true)
	}

	for !userDB.isDone.Load() {
		time.Sleep(waitTime)
	}
}

func bulkUpsert(tmpDB *dmrUserDB) error {
	// SQLITE_LIMIT_VARIABLE_NUMBER
	const limit = 32766
	// Break the users into batches of SQLITE_LIMIT_VARIABLE_NUMBER / 9 (9 is the number of columns in the table)
	// This is to prevent SQLITE_ERROR: too many SQL variables
	batchSize := math.Floor(float64(limit) / 9)
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold: time.Second * 5, // Slow SQL threshold
			LogLevel:      logger.Warn,     // Log level
		},
	)
	tx := userDB.db.Session(&gorm.Session{Logger: newLogger})
	for i := 0; i < len(tmpDB.Users); i += int(batchSize) {
		end := i + int(batchSize)
		if end > len(tmpDB.Users) {
			end = len(tmpDB.Users)
		}
		tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"state", "radio_id", "surname", "city", "callsign", "country", "name", "f_name"}),
		}).CreateInBatches(tmpDB.Users[i:end], len(tmpDB.Users[i:end]))
		if tx.Error != nil {
			return tx.Error
		}
	}
	return nil
}

func Len() int {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	var count int64
	userDB.db.Model(&DMRUser{}).Count(&count)
	if userDB.db.Error != nil {
		logging.Errorf("Error counting users: %v", userDB.db.Error)
		return 0
	}
	return int(count)
}

func Get(dmrID uint) (DMRUser, bool) {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	var user DMRUser
	userDB.db.Find(&user, "id = ?", dmrID)
	if user.ID != dmrID {
		return DMRUser{}, false
	}
	return user, true
}

func Update() error {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	const updateTimeout = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.radioid.net/static/users.json", nil)
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
		logging.Errorf("ReadAll error %s", err)
		return ErrUpdateFailed
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			logging.Errorf("Error closing response body: %v", err)
		}
	}()
	var tmpDB dmrUserDB
	if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
		logging.Errorf("Error decoding DMR users database: %v", err)
		return ErrUpdateFailed
	}

	if len(tmpDB.Users) == 0 {
		logging.Error("No DMR users found in database")
		return ErrUpdateFailed
	}

	userDB.db.FirstOrCreate(&LastUpdate{LastUpdate: time.Now()})
	err = bulkUpsert(&tmpDB)
	if err != nil {
		logging.Errorf("Error bulk inserting users: %v", err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}

	logging.Errorf("Update complete. Loaded %d DMR users", Len())

	return nil
}

func GetDate() time.Time {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	var lastUpdate LastUpdate
	userDB.db.Find(&lastUpdate)
	if lastUpdate.LastUpdate.IsZero() {
		logging.Error("Error loading DMR users database")
		return userDB.builtInDate
	}
	return lastUpdate.LastUpdate
}
