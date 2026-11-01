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

// Package offlinealert emails the users who asked for it when a hotspot or
// repeater goes offline, and when it is back. Only one replica checks in
// each interval, and each email goes out once, whichever replica sends it.
package offlinealert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/connlog"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"gorm.io/gorm"
)

const (
	// Interval is the time between two checks.
	Interval = time.Minute
	// LockKey is the KV key that the replica that checks holds.
	LockKey     = "dmrhub:offlinealert:lock"
	lockTTL     = Interval - 5*time.Second
	problemLead = 5 * time.Minute
)

// Result counts what one check did.
type Result struct {
	Ran     bool
	Offline int
	Back    int
}

// Checker sends the offline alert emails.
type Checker struct {
	cfg    config.Source
	db     *gorm.DB
	kv     kv.KV
	mailer smtp.Mailer

	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewChecker returns a Checker that reads the config from cfg at each
// check.
func NewChecker(cfg config.Source, db *gorm.DB, kvStore kv.KV) *Checker {
	return &Checker{cfg: cfg, db: db, kv: kvStore}
}

// WithMailer makes the emails of c go out through m instead of the SMTP
// settings.
func (c *Checker) WithMailer(m smtp.Mailer) *Checker {
	c.mailer = m
	return c
}

// Start runs a check each Interval until ctx ends or Stop is called.
func (c *Checker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		ticker := time.NewTicker(Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if _, err := c.Run(ctx, time.Now()); err != nil {
				slog.Error("Offline alerts: check failed", "error", err)
			}
		}
	}()
}

// Stop ends the checks that Start runs and waits for the current one.
func (c *Checker) Stop() {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
			<-c.done
		}
	})
}

type row struct {
	models.OfflineAlert
	Email         string
	EmailVerified bool
	Admin         bool
	Callsign      string
	RepeaterCall  string
	RepeaterType  string
	OwnerID       *uint
	LastPing      time.Time
	Cosysop       bool
}

// Run checks every offline alert at now, if this replica takes the lock.
// An alert whose repeater has had no ping for its delay, from a ping
// after the alert was turned on, gets one offline email. An alert whose
// outage was reported gets a back email, when it asked for one, at the
// first ping after the outage began. A move of a repeater between
// replicas, or a drain of one, does not stop its pings, so it sends
// nothing. It sends nothing while SMTP is off, and nothing to a user
// without a verified email address or who no longer manages the
// repeater.
func (c *Checker) Run(ctx context.Context, now time.Time) (Result, error) {
	cfg := c.cfg.Get()
	if !cfg.SMTP.Enabled {
		return Result{}, nil
	}
	ok, err := c.kv.SetNX(ctx, LockKey, "1", lockTTL)
	if err != nil {
		return Result{}, fmt.Errorf("offlinealert: cannot take the lock: %w", err)
	}
	if !ok {
		return Result{}, nil
	}
	res := Result{Ran: true}
	db := c.db.WithContext(ctx)
	var rows []row
	err = db.Table("offline_alerts").
		Select("offline_alerts.*, users.email, users.email_verified, users.admin, users.callsign, repeaters.callsign AS repeater_call, repeaters.type AS repeater_type, repeaters.owner_id, repeaters.last_ping, " +
			"EXISTS (SELECT 1 FROM repeater_cosysops WHERE repeater_cosysops.repeater_id = offline_alerts.repeater_id AND repeater_cosysops.user_id = offline_alerts.user_id AND repeater_cosysops.accepted_at IS NOT NULL) AS cosysop").
		Joins("JOIN users ON users.id = offline_alerts.user_id AND users.deleted_at IS NULL").
		Joins("JOIN repeaters ON repeaters.id = offline_alerts.repeater_id AND repeaters.deleted_at IS NULL").
		Scan(&rows).Error
	if err != nil {
		return res, fmt.Errorf("offlinealert: cannot list the alerts: %w", err)
	}
	mailer := c.mailer
	if mailer == nil {
		mailer = smtp.NewMailer(cfg)
	}
	for i := range rows {
		r := &rows[i]
		if r.Email == "" || !r.EmailVerified || !manages(r) || r.LastPing.IsZero() {
			continue
		}
		switch {
		case r.SentAt == nil && r.LastPing.After(r.CreatedAt) && now.Sub(r.LastPing) >= time.Duration(r.DelayMinutes)*time.Minute: //nolint:gosec // a delay is at most 60
			sent, err := c.offline(db, cfg, mailer, r, now)
			if err != nil {
				slog.Error("Offline alerts: could not send an offline email", "repeaterID", r.RepeaterID, "user", r.UserID, "error", err)
			}
			if sent {
				res.Offline++
			}
		case r.SentAt != nil && r.Since != nil && r.LastPing.After(*r.Since):
			sent, err := c.back(db, cfg, mailer, r)
			if err != nil {
				slog.Error("Offline alerts: could not send a back email", "repeaterID", r.RepeaterID, "user", r.UserID, "error", err)
			}
			if sent {
				res.Back++
			}
		}
	}
	return res, nil
}

func manages(r *row) bool {
	return r.Admin || r.Cosysop || (r.OwnerID != nil && *r.OwnerID == r.UserID)
}

func (c *Checker) offline(db *gorm.DB, cfg *config.Config, mailer smtp.Mailer, r *row, now time.Time) (bool, error) {
	claimed, err := models.ClaimOfflineAlert(db, &r.OfflineAlert, r.LastPing, now)
	if err != nil || !claimed {
		return false, err //nolint:wrapcheck
	}
	problem := ""
	if ev, ok, err := models.LastRepeaterProblem(db, r.RepeaterID, r.LastPing.Add(-problemLead), connlog.Problems()); err != nil {
		slog.Error("Offline alerts: could not read the connection log", "repeaterID", r.RepeaterID, "error", err)
	} else if ok {
		problem = connlog.Sentence(ev.Kind, ev.Detail, device(r))
	}
	subject, body := OfflineEmail(cfg, name(r), device(r), r.RepeaterID, r.LastPing, now, problem)
	if err := mailer.SendText(r.Email, subject, body); err != nil {
		if _, rerr := models.ReleaseOfflineAlert(db, &r.OfflineAlert); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return false, err //nolint:wrapcheck
	}
	return true, nil
}

func (c *Checker) back(db *gorm.DB, cfg *config.Config, mailer smtp.Mailer, r *row) (bool, error) {
	released, err := models.ReleaseOfflineAlert(db, &r.OfflineAlert)
	if err != nil || !released || !r.Back {
		return false, err //nolint:wrapcheck
	}
	subject, body := BackEmail(cfg, name(r), device(r), r.RepeaterID, *r.Since, r.LastPing)
	if err := mailer.SendText(r.Email, subject, body); err != nil {
		return false, err //nolint:wrapcheck
	}
	return true, nil
}

func device(r *row) string {
	if r.RepeaterType == models.RepeaterTypeIPSC || r.RepeaterID < 1000000 {
		return "repeater"
	}
	return "hotspot"
}

func name(r *row) string {
	if r.RepeaterCall == "" {
		return fmt.Sprint(r.RepeaterID)
	}
	return fmt.Sprintf("%s %d", r.RepeaterCall, r.RepeaterID)
}

func pageOf(cfg *config.Config, id uint) string {
	return fmt.Sprintf("%s/repeaters/%d", strings.TrimRight(cfg.HTTP.CanonicalHost, "/"), id)
}

func minutes(d time.Duration) string {
	m := int(d / time.Minute)
	if m == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", m)
}

// OfflineEmail returns the subject and the body of the email about the
// device called name with the ID id, offline since its last ping at since,
// at now. problem, when not empty, is the last problem of its connection
// log.
func OfflineEmail(cfg *config.Config, name, device string, id uint, since, now time.Time, problem string) (string, string) {
	subject := fmt.Sprintf("%s %s is offline", upper(device), name)
	var b strings.Builder
	fmt.Fprintf(&b, "Your %s %s has been offline on %s since %s UTC (%s).\n\n", device, name, cfg.NetworkName, since.UTC().Format("Jan 2, 15:04"), minutes(now.Sub(since)))
	if problem != "" {
		fmt.Fprintf(&b, "Last problem: %s\n\n", problem)
	}
	fmt.Fprintf(&b, "Open the connection log: %s\n\n", pageOf(cfg, id))
	fmt.Fprintf(&b, "To stop these emails, turn off \"Email me when it is offline\" on the same page.\n")
	return subject, b.String()
}

// BackEmail returns the subject and the body of the email about the
// device called name with the ID id, back at back after an outage that
// began at since.
func BackEmail(cfg *config.Config, name, device string, id uint, since, back time.Time) (string, string) {
	subject := fmt.Sprintf("%s %s is back", upper(device), name)
	body := fmt.Sprintf("Your %s %s is back on %s since %s UTC. It was offline for %s.\n\n"+
		"Open the connection log: %s\n\n"+
		"To stop these emails, turn off \"Email me when it is offline\" on the same page.\n",
		device, name, cfg.NetworkName, back.UTC().Format("Jan 2, 15:04"), minutes(back.Sub(since)), pageOf(cfg, id))
	return subject, body
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
