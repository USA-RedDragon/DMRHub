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

// Package radioiddrift finds the users whose callsign RadioID no longer
// gives their DMR ID, once a night, on one replica.
package radioiddrift

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"gorm.io/gorm"
)

const (
	// LockKey is the KV key that lets one replica run the check each night.
	LockKey = "dmrhub:radioid-drift:lock"
	// RunHour is the local hour of the check, after the RadioID update at
	// midnight.
	RunHour   = 1
	lockTTL   = 23 * time.Hour
	batchSize = 1000
)

// ErrNoRadioIDData is the error of a check while no RadioID data is
// loaded, which would find every user.
var ErrNoRadioIDData = errors.New("radioiddrift: no RadioID data is loaded")

// Lookup returns the callsign that RadioID gives dmrID.
type Lookup func(dmrID uint) (callsign string, ok bool)

// Result is what a check did.
type Result struct {
	// Ran reports that this replica ran the check.
	Ran bool
	// Users is the number of users checked.
	Users int
	// Drifts is the number of users found.
	Drifts int
}

// Checker runs the RadioID drift check.
type Checker struct {
	cfg    config.Source
	db     *gorm.DB
	kv     kv.KV
	lookup Lookup
	size   func() int

	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewChecker returns a Checker that compares the users with the loaded
// RadioID data.
func NewChecker(cfg config.Source, db *gorm.DB, kvStore kv.KV) *Checker {
	return NewCheckerWith(cfg, db, kvStore, func(id uint) (string, bool) {
		u, ok := userdb.Get(id)
		if !ok || u.ID != id {
			return "", false
		}
		return u.Callsign, true
	}, userdb.Len)
}

// NewCheckerWith returns a Checker that compares the users with lookup,
// which has size entries.
func NewCheckerWith(cfg config.Source, db *gorm.DB, kvStore kv.KV, lookup Lookup, size func() int) *Checker {
	return &Checker{cfg: cfg, db: db, kv: kvStore, lookup: lookup, size: size}
}

// NextRun returns the first RunHour o'clock after now, in the zone of now.
func NextRun(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), RunHour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, RunHour, 0, 0, 0, now.Location())
	}
	return next
}

// Start runs the check at each NextRun until ctx ends or Stop is called.
func (c *Checker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		for {
			timer := time.NewTimer(time.Until(NextRun(time.Now())))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			res, err := c.Run(ctx, time.Now())
			if err != nil {
				slog.Error("RadioID drift: check failed", "error", err)
				continue
			}
			if res.Ran {
				slog.Info("RadioID drift: checked the users", "users", res.Users, "drifted", res.Drifts)
			}
		}
	}()
}

// Stop ends the checks that Start runs.
func (c *Checker) Stop() {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
			<-c.done
		}
	})
}

// Run checks every user who is not deleted and whose callsign no admin
// chose, and makes the users found the RadioID drift list. It does
// nothing when another replica ran it in the last 23 hours. Without radio
// ID validation it empties the list.
func (c *Checker) Run(ctx context.Context, now time.Time) (Result, error) {
	db := c.db.WithContext(ctx)
	if c.cfg.Get().DMR.DisableRadioIDValidation {
		if err := models.ReplaceRadioIDDrifts(db, nil, now); err != nil {
			return Result{}, fmt.Errorf("radioiddrift: %w", err)
		}
		return Result{}, nil
	}
	if c.size() == 0 {
		return Result{}, ErrNoRadioIDData
	}
	ok, err := c.kv.SetNX(ctx, LockKey, "1", lockTTL)
	if err != nil {
		return Result{}, fmt.Errorf("radioiddrift: cannot take the lock: %w", err)
	}
	if !ok {
		return Result{}, nil
	}
	res := Result{Ran: true}
	var found []models.RadioIDDrift
	var last uint
	for {
		var users []models.User
		err := db.Select("id", "callsign", "callsign_override").Where("id > ?", last).Order("id").Limit(batchSize).Find(&users).Error
		if err != nil {
			return res, fmt.Errorf("radioiddrift: cannot list the users: %w", err)
		}
		for _, u := range users {
			if u.CallsignOverride {
				continue
			}
			res.Users++
			callsign, ok := c.lookup(u.ID)
			if ok && strings.EqualFold(callsign, u.Callsign) {
				continue
			}
			found = append(found, models.RadioIDDrift{UserID: u.ID, RadioIDCallsign: strings.ToUpper(callsign)})
		}
		if len(users) < batchSize {
			break
		}
		last = users[len(users)-1].ID
	}
	if err := models.ReplaceRadioIDDrifts(db, found, now); err != nil {
		return res, fmt.Errorf("radioiddrift: %w", err)
	}
	res.Drifts = len(found)
	return res, nil
}
