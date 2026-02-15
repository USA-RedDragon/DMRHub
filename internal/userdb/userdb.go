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
	"bytes"
	"context"
	// Embed the users.json.xz file into the binary.
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

//go:embed userdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/users.json
//
//go:embed users.json.xz
var compressedDMRUsersDB []byte

var userDB UserDB //nolint:gochecknoglobals

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

type dbMetadata struct {
	Count int
	Date  time.Time
}

type UserDB struct {
	metadata           atomic.Value // stores dbMetadata
	dmrUserMap         *xsync.Map[uint, DMRUser]
	dmrUserMapUpdating *xsync.Map[uint, DMRUser]

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
			slog.Error("Error unpacking database", "error", err)
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
	lastInit := userDB.isInited.Swap(true)
	if !lastInit {
		userDB.dmrUserMapUpdating = xsync.NewMap[uint, DMRUser](xsync.WithPresize(250000), xsync.WithGrowOnly())

		var err error
		userDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			return ErrParsingDate
		}
		dbReader, err := xz.NewReader(bytes.NewReader(compressedDMRUsersDB))
		if err != nil {
			return ErrXZReader
		}

		count, err := streamDecodeUsers(json.NewDecoder(dbReader), userDB.dmrUserMapUpdating)
		if err != nil {
			return err
		}
		if count == 0 {
			slog.Error("No DMR users found in database")
			return ErrNoUsers
		}

		userDB.metadata.Store(dbMetadata{Count: count, Date: userDB.builtInDate})
		userDB.dmrUserMap = userDB.dmrUserMapUpdating
		userDB.dmrUserMapUpdating = xsync.NewMap[uint, DMRUser]()
		userDB.isDone.Store(true)
	}

	for !userDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	meta, ok := userDB.metadata.Load().(dbMetadata)
	if !ok {
		slog.Error("Error loading DMR users database")
		return ErrLoading
	}
	if meta.Count == 0 {
		slog.Error("No DMR users found in database")
		return ErrNoUsers
	}
	return nil
}

func Len() int {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return 0
		}
	}
	meta, ok := userDB.metadata.Load().(dbMetadata)
	if !ok {
		slog.Error("Error loading DMR users database")
		return 0
	}
	return meta.Count
}

func Get(dmrID uint) (DMRUser, bool) {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			slog.Error("Error unpacking database", "error", err)
			return DMRUser{}, false
		}
	}
	user, ok := userDB.dmrUserMap.Load(dmrID)
	if !ok {
		return DMRUser{}, false
	}
	return user, true
}

func Update(url string) error {
	if !userDB.isDone.Load() {
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

	userDB.dmrUserMapUpdating = xsync.NewMap[uint, DMRUser](xsync.WithPresize(Len()), xsync.WithGrowOnly())
	count, err := streamDecodeUsers(json.NewDecoder(resp.Body), userDB.dmrUserMapUpdating)
	if err != nil {
		slog.Error("Error decoding DMR users database", "error", err)
		return ErrUpdateFailed
	}

	if count == 0 {
		slog.Error("No DMR users found in database")
		return ErrUpdateFailed
	}

	userDB.metadata.Store(dbMetadata{Count: count, Date: time.Now()})
	userDB.dmrUserMap = userDB.dmrUserMapUpdating
	userDB.dmrUserMapUpdating = xsync.NewMap[uint, DMRUser]()

	slog.Info("Update complete", "loadedUsers", Len())

	return nil
}

func GetDate() (time.Time, error) {
	if !userDB.isDone.Load() {
		err := UnpackDB()
		if err != nil {
			return time.Time{}, err
		}
	}
	meta, ok := userDB.metadata.Load().(dbMetadata)
	if !ok {
		slog.Error("Error loading DMR users database")
		return time.Time{}, ErrLoading
	}
	return meta.Date, nil
}
