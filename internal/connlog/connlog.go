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

// Package connlog keeps the connection logs of the repeaters: what the
// protocol servers saw of their logins and sessions, in plain kinds that
// the repeater page explains. It writes off the caller's goroutine and
// folds repeats, so that a flood of failed logins costs a row update.
package connlog

import (
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

// The kinds of the events of a connection log.
const (
	// Connected is a login that the hub took. Detail is the software of
	// the repeater.
	Connected = "connected"
	// WrongPassword is a login refused for a password or auth key that
	// does not match.
	WrongPassword = "wrong_password"
	// IPBlocked is a packet from an address that an IP list refuses.
	IPBlocked = "ip_blocked"
	// BadConfig is a configuration packet that the hub cannot read.
	BadConfig = "bad_config"
	// PushedOff is a session that another login with the same ID took
	// over. Address is the address of the other login.
	PushedOff = "pushed_off"
	// Timeout is a session that ended with no packet from the repeater.
	Timeout = "timeout"
	// Closed is a session that the repeater logged out of.
	Closed = "closed"
	// PasswordRotated is a session that the hub ended after a change of
	// the password or auth key.
	PasswordRotated = "password_rotated"
	// Reset is a session that a sysop ended. Detail is the callsign of
	// the sysop.
	Reset = "reset"
	// BadOptions is an options text with a value that is not valid.
	// Detail is the error.
	BadOptions = "bad_options"
	// UnknownTalkgroup is an options text that names talkgroups that the
	// hub does not have. Detail lists them.
	UnknownTalkgroup = "unknown_talkgroup"
	// OptionsBusy is an options text that the hub could not take at once.
	OptionsBusy = "options_busy"
)

// Problems returns the kinds that need a change by the sysop or show a
// fault.
func Problems() []string {
	return []string{WrongPassword, IPBlocked, BadConfig, PushedOff, Timeout, BadOptions, UnknownTalkgroup, OptionsBusy}
}

const maxPending = 1024

type entry struct {
	ev    models.RepeaterEvent
	count uint
}

// Log writes the events of the connection logs to the database.
type Log struct {
	db *gorm.DB

	mu      sync.Mutex
	pending []entry
	writing bool
	closed  bool
	wg      sync.WaitGroup
}

// New returns a Log that writes to db. A nil db makes a Log that drops
// every event.
func New(db *gorm.DB) *Log {
	return &Log{db: db}
}

// Note adds an event of kind from address with detail to the connection
// log of the repeater, as of the time at. It does not wait for the database. An
// event that repeats the last one of the repeater that waits is folded
// into it.
func (l *Log) Note(repeaterID uint, kind, address, detail string, at time.Time) {
	if l == nil || l.db == nil || repeaterID == 0 {
		return
	}
	if len(detail) > 255 {
		detail = detail[:255]
	}
	if len(address) > 64 {
		address = address[:64]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	for i := len(l.pending) - 1; i >= 0; i-- {
		p := &l.pending[i]
		if p.ev.RepeaterID != repeaterID {
			continue
		}
		if p.ev.Kind == kind && p.ev.Address == address && p.ev.Detail == detail {
			p.count++
			if at.After(p.ev.LastAt) {
				p.ev.LastAt = at
			}
			return
		}
		break
	}
	if len(l.pending) >= maxPending {
		slog.Debug("Dropping a connection log event while the log is behind", "repeaterID", repeaterID, "kind", kind)
		return
	}
	l.pending = append(l.pending, entry{ev: models.RepeaterEvent{RepeaterID: repeaterID, Kind: kind, Address: address, Detail: detail, FirstAt: at, LastAt: at}, count: 1})
	if !l.writing {
		l.writing = true
		l.wg.Add(1)
		go l.write()
	}
}

func (l *Log) write() {
	defer l.wg.Done()
	for {
		l.mu.Lock()
		batch := l.pending
		l.pending = nil
		if len(batch) == 0 {
			l.writing = false
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		for _, e := range batch {
			if _, err := models.NoteRepeaterEvent(l.db, e.ev, e.count); err != nil {
				slog.Error("Error writing the connection log", "repeaterID", e.ev.RepeaterID, "kind", e.ev.Kind, "error", err)
			}
		}
	}
}

// Wait waits until the events noted before it are written.
func (l *Log) Wait() {
	if l == nil {
		return
	}
	l.wg.Wait()
}

// Close writes the events that wait and drops those noted after it.
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.wg.Wait()
}

// Sentence returns the plain sentence of an event of kind with detail on
// a device, a hotspot or a repeater, and for a problem what to change, as
// the repeater page shows them.
func Sentence(kind, detail, device string) string {
	switch kind {
	case WrongPassword:
		return "Login refused: wrong password. The password in the " + device + " does not match."
	case IPBlocked:
		return "Login refused: the address is blocked. Ask an admin of the network."
	case BadConfig:
		return "Login refused: bad configuration packet. Check the callsign, frequencies and location in the settings of the " + device + "."
	case PushedOff:
		return "Pushed off by another login with the same ID. Give each device its own ID."
	case Timeout:
		if detail != "" {
			return "Connection lost: no ping for " + detail + " seconds. Check the internet connection of the " + device + "."
		}
		return "Connection lost. Check the internet connection of the " + device + "."
	case BadOptions:
		return "The options of the " + device + " have a value that is not valid. Check its Options line."
	case UnknownTalkgroup:
		return "Talkgroup " + detail + " in the " + device + " options does not exist."
	case OptionsBusy:
		return "The options waited: the network was busy."
	}
	return ""
}
