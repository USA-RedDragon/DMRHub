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

package userdb

import (
	"bytes"
	"context"
	// Embed the users.json.xz file into the binary.
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
	ErrLoading      = errors.New("error loading DMR users database")
	ErrNoUsers      = errors.New("no DMR users found in database")
	ErrParsingDate  = errors.New("error parsing built-in date")
	ErrXZReader     = errors.New("error creating xz reader")
	ErrReadDB       = errors.New("error reading database")
	ErrDecodingDB   = errors.New("error decoding DMR users database")
)

const waitTime = 100 * time.Millisecond

type UserDB struct {
	uncompressedJSON   []byte
	dmrUsers           atomic.Value
	dmrUserMap         *xsync.MapOf[uint, DMRUser]
	dmrUserMapUpdating *xsync.MapOf[uint, DMRUser]

	builtInDate time.Time
	isInited    atomic.Bool
	isDone      atomic.Bool
}

type dmrUserDB struct {
	Users []DMRUser `json:"users"`
	Date  time.Time `json:"-"`
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

func IsValidUserID(dmrID uint) bool {
	// Check that the user id is 7 digits
	if dmrID < 1000000 || dmrID > 9999999 {
		return false
	}
	return true
}

func ValidUserCallsign(dmrID uint, callsign string) bool {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return false
		}
	}
	user, ok := userDB.dmrUserMap.Load(dmrID)
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

func (e *dmrUserDB) Unmarshal(b []byte) error {
	err := json.Unmarshal(b, e)
	if err != nil {
		return ErrUnmarshal
	}
	return nil
}

func UnpackDB() error {
	lastInit := userDB.isInited.Swap(true)
	if !lastInit {
		userDB.dmrUserMap = xsync.NewMapOf[uint, DMRUser]()
		userDB.dmrUserMapUpdating = xsync.NewMapOf[uint, DMRUser]()

		var err error
		userDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			return ErrParsingDate
		}
		dbReader, err := xz.NewReader(bytes.NewReader(compressedDMRUsersDB))
		if err != nil {
			return ErrXZReader
		}
		userDB.uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			return ErrReadDB
		}
		var tmpDB dmrUserDB
		if err := json.Unmarshal(userDB.uncompressedJSON, &tmpDB); err != nil {
			return ErrDecodingDB
		}
		tmpDB.Date = userDB.builtInDate
		userDB.dmrUsers.Store(tmpDB)
		for i := range tmpDB.Users {
			userDB.dmrUserMapUpdating.Store(tmpDB.Users[i].ID, tmpDB.Users[i])
		}

		userDB.dmrUserMap = userDB.dmrUserMapUpdating
		userDB.dmrUserMapUpdating = xsync.NewMapOf[uint, DMRUser]()
		userDB.isDone.Store(true)
	}

	for !userDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	usrdb, ok := userDB.dmrUsers.Load().(dmrUserDB)
	if !ok {
		logging.Error("Error loading DMR users database")
		return ErrLoading
	}
	if len(usrdb.Users) == 0 {
		logging.Error("No DMR users found in database")
		return ErrNoUsers
	}
	return nil
}

func Len() int {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return 0
		}
	}
	db, ok := userDB.dmrUsers.Load().(dmrUserDB)
	if !ok {
		logging.Error("Error loading DMR users database")
	}
	return len(db.Users)
}

func Get(dmrID uint) (DMRUser, bool) {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return DMRUser{}, false
		}
	}
	user, ok := userDB.dmrUserMap.Load(dmrID)
	if !ok {
		return DMRUser{}, false
	}
	return user, true
}

func Update() error {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			logging.Errorf("Error unpacking database: %v", err)
			return ErrUpdateFailed
		}
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

	userDB.uncompressedJSON, err = io.ReadAll(resp.Body)
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
	if err := json.Unmarshal(userDB.uncompressedJSON, &tmpDB); err != nil {
		logging.Errorf("Error decoding DMR users database: %v", err)
		return ErrUpdateFailed
	}

	if len(tmpDB.Users) == 0 {
		logging.Error("No DMR users found in database")
		return ErrUpdateFailed
	}

	tmpDB.Date = time.Now()
	userDB.dmrUsers.Store(tmpDB)

	userDB.dmrUserMapUpdating = xsync.NewMapOf[uint, DMRUser]()
	for i := range tmpDB.Users {
		userDB.dmrUserMapUpdating.Store(tmpDB.Users[i].ID, tmpDB.Users[i])
	}

	userDB.dmrUserMap = userDB.dmrUserMapUpdating
	userDB.dmrUserMapUpdating = xsync.NewMapOf[uint, DMRUser]()

	logging.Errorf("Update complete. Loaded %d DMR users", Len())

	return nil
}

func GetDate() (time.Time, error) {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			return time.Time{}, err
		}
	}
	db, ok := userDB.dmrUsers.Load().(dmrUserDB)
	if !ok {
		logging.Error("Error loading DMR users database")
	}
	return db.Date, nil
}
