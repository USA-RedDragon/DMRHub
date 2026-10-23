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

// Package locator is the location server of the hub: the radio IDs 5050
// to 5059 that BrandMeister uses for ARS and location. It answers the data
// that radios send to those IDs, as the destination station does, and
// stores the positions of the radios whose owners turned tracking on.
package locator

import (
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/location"
	"gorm.io/gorm"
)

const (
	firstService = 5050
	lastService  = 5059

	tickInterval = 50 * time.Millisecond
	linkIdle     = 2 * time.Minute
	networkLost  = 500 * time.Millisecond
)

// IsService reports whether id is a radio ID of the location server.
func IsService(id uint) bool {
	return id >= firstService && id <= lastService
}

type linkKey struct {
	radio, service uint
}

type link struct {
	rx       *data.Receiver
	repeater uint
	slot     bool
	last     time.Time
}

// Locator is the location server. Its methods are safe for concurrent use.
type Locator struct {
	db     *gorm.DB
	send   func(models.Packet)
	epoch  time.Time
	mu     sync.Mutex
	links  map[linkKey]*link
	dec    location.Decoder
	events []data.Event
	stream uint
	done   chan struct{}
	wg     sync.WaitGroup
}

// New returns a Locator that stores positions in db and hands the packets
// it sends to send, which routes each to its Repeater.
func New(db *gorm.DB, send func(models.Packet)) *Locator {
	l := &Locator{
		db:     db,
		send:   send,
		epoch:  time.Now(),
		links:  map[linkKey]*link{},
		stream: 0x10C0_0000,
		done:   make(chan struct{}),
	}
	l.wg.Add(1)
	go l.tick()
	return l
}

// Stop ends the timers of the Locator.
func (l *Locator) Stop() {
	close(l.done)
	l.wg.Wait()
}

func (l *Locator) now() time.Duration {
	return time.Since(l.epoch)
}

// Handle takes a private burst to a radio ID of the location server.
func (l *Locator) Handle(p models.Packet) {
	if p.GroupCall || !IsService(p.Dst) || p.FrameType != dmrconst.FrameDataSync {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := linkKey{radio: p.Src, service: p.Dst}
	ln, ok := l.links[k]
	if !ok {
		rx, err := data.NewReceiver(&data.ReceiverConfig{
			Address: uint32(p.Dst), //nolint:gosec
			Respond: true,
			Source:  layer2.SyncSourceBS,
			Lost:    networkLost,
		})
		if err != nil {
			slog.Error("Locator: cannot make a data receiver", "error", err)
			return
		}
		ln = &link{rx: rx}
		l.links[k] = ln
	}
	ln.repeater, ln.slot, ln.last = p.Repeater, p.Slot, time.Now()
	now := l.now()
	d := p.Data()
	l.events = ln.rx.Burst(now, (*layer2.RawBurst)(&p.DMRData), d.Hint(now), l.events[:0])
	l.apply(k, ln, now)
}

func (l *Locator) apply(k linkKey, ln *link, now time.Duration) {
	for i := range l.events {
		e := &l.events[i]
		switch e.Kind { //nolint:exhaustive
		case data.EventRespond:
			l.respond(k, ln, e.Burst, e.Due-now)
		case data.EventDelivered, data.EventReassembled:
			l.deliver(k, ln, &e.Message)
		}
	}
}

func (l *Locator) respond(k linkKey, ln *link, raw layer2.RawBurst, wait time.Duration) {
	l.stream++
	p := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         k.service,
		Dst:         k.radio,
		Repeater:    ln.repeater,
		Slot:        ln.slot,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(raw.SlotType().DataType),
		StreamID:    l.stream,
		DMRData:     raw,
		BER:         -1,
		RSSI:        -1,
	}
	if wait <= 0 {
		l.send(p)
		return
	}
	time.AfterFunc(wait, func() { l.send(p) })
}

func (l *Locator) deliver(k linkKey, ln *link, m *data.Message) {
	if m.BadCRC {
		return
	}
	f, err := l.dec.Decode(&m.Header, m.Payload)
	if err != nil {
		if !errors.Is(err, location.ErrNoFix) && !errors.Is(err, location.ErrNoPosition) {
			slog.Debug("Locator: a report does not decode", "radio", k.radio, "error", err)
		}
		return
	}
	pos, ok := positionOf(&f, k, ln)
	if !ok {
		return
	}
	if _, err := models.StorePosition(l.db, pos); err != nil {
		slog.Error("Locator: cannot store a position", "radio", k.radio, "error", err)
	}
}

func positionOf(f *location.Fix, k linkKey, ln *link) (models.Position, bool) {
	if !f.Valid || math.IsNaN(f.Latitude) || math.IsNaN(f.Longitude) ||
		f.Latitude < -90 || f.Latitude > 90 || f.Longitude < -180 || f.Longitude > 180 ||
		(f.Latitude == 0 && f.Longitude == 0) {
		return models.Position{}, false
	}
	p := models.Position{
		RadioID:    k.radio,
		Latitude:   f.Latitude,
		Longitude:  f.Longitude,
		ReceivedAt: time.Now(),
		RepeaterID: ln.repeater,
		Slot:       1,
		ServiceID:  k.service,
	}
	if ln.slot {
		p.Slot = 2
	}
	switch f.Format {
	case location.FormatLRRP:
		p.Format = models.PositionFormatLRRP
	case location.FormatNMEA:
		p.Format = models.PositionFormatNMEA
	case location.FormatHDAP:
		p.Format = models.PositionFormatHDAP
	}
	if f.HasRadius {
		p.AccuracyM = &f.Radius
	}
	if f.HasSpeed {
		p.SpeedMPS = &f.Speed
	}
	if f.HasCourse {
		p.HeadingDeg = &f.Course
	}
	if f.HasAltitude {
		p.AltitudeM = &f.Altitude
	}
	if f.HasTime {
		at := time.Date(int(f.Year), time.Month(f.Month), int(f.Day), int(f.Hour), int(f.Minute), int(f.Second), 0, time.UTC)
		p.FixTime = &at
	}
	return p, true
}

func (l *Locator) tick() {
	defer l.wg.Done()
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
			l.expire()
		}
	}
}

func (l *Locator) expire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, ln := range l.links {
		if at, ok := ln.rx.Deadline(); ok && now >= at {
			l.events = ln.rx.Tick(now, l.events[:0])
			l.apply(k, ln, now)
		}
		if time.Since(ln.last) > linkIdle {
			delete(l.links, k)
		}
	}
}
