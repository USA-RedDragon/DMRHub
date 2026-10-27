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
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"gorm.io/gorm"
)

const (
	// LockKey is the KV key that lets one replica run the check each night.
	LockKey = "dmrhub:radioid-drift:lock"
	// RunHour is the local hour of the check, after the RadioID update at
	// midnight.
	RunHour = 1
	// MailFloor and MailPercent guard the users against a bad RadioID
	// download: when more than MailFloor radio IDs and more than
	// MailPercent percent of the checked radio IDs drift in one check, the
	// check emails nobody.
	MailFloor   = 5
	MailPercent = 5
	lockTTL     = 23 * time.Hour
	batchSize   = 1000
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
	// Checked is the number of radio IDs checked.
	Checked int
	// Drifts is the number of radio IDs found.
	Drifts int
	// Notified is the number of emails sent.
	Notified int
}

// Checker runs the RadioID drift check.
type Checker struct {
	cfg    config.Source
	db     *gorm.DB
	kv     kv.KV
	lookup Lookup
	size   func() int
	mailer smtp.Mailer

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

// WithMailer makes the emails of c go out through m instead of the SMTP
// settings.
func (c *Checker) WithMailer(m smtp.Mailer) *Checker {
	c.mailer = m
	return c
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
				slog.Info("RadioID drift: checked the radio IDs", "checked", res.Checked, "drifted", res.Drifts, "emailed", res.Notified)
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

// Run checks the account ID and the added radio IDs of every user who is
// not deleted and whose callsign no admin chose, and makes the radio IDs
// that RadioID does not list under the callsign of their user the RadioID
// drift list. It emails each user once about each radio ID found, when
// SMTP is on and the user verified an address. It does nothing when
// another replica ran it in the last 23 hours. Without radio ID
// validation it empties the list.
func (c *Checker) Run(ctx context.Context, now time.Time) (Result, error) {
	db := c.db.WithContext(ctx)
	cfg := c.cfg.Get()
	if cfg.DMR.DisableRadioIDValidation {
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
	found, checked, err := c.find(db)
	if err != nil {
		return res, err
	}
	res.Checked = checked
	if err := models.ReplaceRadioIDDrifts(db, found, now); err != nil {
		return res, fmt.Errorf("radioiddrift: %w", err)
	}
	res.Drifts = len(found)
	if !cfg.SMTP.Enabled {
		return res, nil
	}
	if res.Drifts > MailFloor && res.Drifts*100 > MailPercent*res.Checked {
		slog.Warn("RadioID drift: too many radio IDs drift for a real change, so no user gets an email; check the RadioID data", "checked", res.Checked, "drifted", res.Drifts)
		return res, nil
	}
	res.Notified, err = c.notify(db, cfg, now)
	return res, err
}

func (c *Checker) find(db *gorm.DB) ([]models.RadioIDDrift, int, error) {
	var found []models.RadioIDDrift
	checked := 0
	check := func(radioID, userID uint, callsign string) {
		checked++
		listed, ok := c.lookup(radioID)
		if ok && strings.EqualFold(listed, callsign) {
			return
		}
		found = append(found, models.RadioIDDrift{RadioID: radioID, UserID: userID, RadioIDCallsign: strings.ToUpper(listed)})
	}
	var last uint
	for {
		var users []models.User
		err := db.Select("id", "callsign", "callsign_override").Where("id > ?", last).Order("id").Limit(batchSize).Find(&users).Error
		if err != nil {
			return nil, 0, fmt.Errorf("radioiddrift: cannot list the users: %w", err)
		}
		ids := make([]uint, 0, len(users))
		callsigns := make(map[uint]string, len(users))
		for _, u := range users {
			if u.CallsignOverride {
				continue
			}
			ids = append(ids, u.ID)
			callsigns[u.ID] = u.Callsign
			check(u.ID, u.ID, u.Callsign)
		}
		if len(ids) > 0 {
			var extra []models.UserRadioID
			if err := db.Select("user_id", "radio_id").Where("user_id IN ?", ids).Order("radio_id").Find(&extra).Error; err != nil {
				return nil, 0, fmt.Errorf("radioiddrift: cannot list the radio IDs: %w", err)
			}
			for _, r := range extra {
				check(r.RadioID, r.UserID, callsigns[r.UserID])
			}
		}
		if len(users) < batchSize {
			return found, checked, nil
		}
		last = users[len(users)-1].ID
	}
}

func (c *Checker) notify(db *gorm.DB, cfg *config.Config, now time.Time) (int, error) {
	rows, err := models.UnnotifiedRadioIDDrifts(db)
	if err != nil {
		return 0, fmt.Errorf("radioiddrift: %w", err)
	}
	mailer := c.mailer
	if mailer == nil {
		mailer = smtp.NewMailer(cfg)
	}
	sent := 0
	for _, r := range rows {
		if err := mailer.SendText(r.Email, "Your callsign on "+cfg.NetworkName+" does not match RadioID", body(cfg, r)); err != nil {
			slog.Error("RadioID drift: could not email a user", "user", r.UserID, "radio_id", r.RadioID, "error", err)
			continue
		}
		if err := models.MarkRadioIDDriftNotified(db, r.RadioID, now); err != nil {
			return sent, fmt.Errorf("radioiddrift: %w", err)
		}
		sent++
	}
	return sent, nil
}

func body(cfg *config.Config, r models.RadioIDDriftRow) string {
	kind := "radio ID"
	if r.RadioID == r.UserID {
		kind = "DMR ID"
	}
	listed := "does not list " + kind + fmt.Sprint(" ", r.RadioID)
	if r.RadioIDCallsign != "" {
		listed = fmt.Sprintf("lists %s %d under %s", kind, r.RadioID, r.RadioIDCallsign)
	}
	return fmt.Sprintf("RadioID %s, but your callsign on %s is %s.\n\n"+
		"If your callsign changed, update it under Settings on %s, or ask an admin of %s.\n",
		listed, cfg.NetworkName, r.Callsign, cfg.NetworkName, cfg.NetworkName)
}
