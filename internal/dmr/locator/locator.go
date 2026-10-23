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
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/location"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/ars"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	firstService = 5050
	lastService  = 5059

	tickInterval = 50 * time.Millisecond
	recheckEvery = time.Second
	silentAfter  = 3
	heardExpiry  = time.Hour
	maxInterval  = 24 * time.Hour
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
	tx       *data.Sender
	queue    []data.Message
	repeater uint
	slot     bool
	cc       enums.ColorCode
	last     time.Time
}

// Locator is the location server. Its methods are safe for concurrent use.
type Locator struct {
	db      *gorm.DB
	send    func(models.Packet)
	epoch   time.Time
	mu      sync.Mutex
	links   map[linkKey]*link
	dec     location.Decoder
	events  []data.Event
	outs    []data.Output
	lrrp    lrrp.Message
	reqID   uint32
	stream  uint
	heard   map[uint]heardAt
	retried map[uint]bool
	checked time.Time
	done    chan struct{}
	wg      sync.WaitGroup
}

// New returns a Locator that stores positions in db and hands the packets
// it sends to send, which routes each to its Repeater.
func New(db *gorm.DB, send func(models.Packet)) *Locator {
	l := &Locator{
		db:      db,
		send:    send,
		epoch:   time.Now(),
		links:   map[linkKey]*link{},
		heard:   map[uint]heardAt{},
		retried: map[uint]bool{},
		stream:  0x10C0_0000,
		done:    make(chan struct{}),
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

type heardAt struct {
	repeater uint
	slot     bool
	at       time.Time
}

// Heard tells the Locator that the radio was heard on repeater and slot,
// so that it can ask a tracked radio that went silent for its position
// again there.
func (l *Locator) Heard(radio, repeater uint, slot bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.heard[radio] = heardAt{repeater: repeater, slot: slot, at: time.Now()}
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
	ln.cc = layer2.RawBurst(p.DMRData).SlotType().ColorCode
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
	if sap, ok := m.Header.SAP(); ok {
		if d, err := ip.Decode(sap, m.Payload); err == nil && d.DestinationPort == ip.PortARS {
			l.registration(k, ln, d.Payload)
			return
		}
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
	delete(l.retried, k.radio)
	if _, err := models.StorePosition(l.db, pos); err != nil {
		slog.Error("Locator: cannot store a position", "radio", k.radio, "error", err)
	}
}

func (l *Locator) registration(k linkKey, ln *link, payload []byte) {
	m, err := ars.DecodeMessage(payload)
	if err != nil {
		slog.Debug("Locator: an ARS message does not decode", "radio", k.radio, "error", err)
		return
	}
	switch m.Header.Type { //nolint:exhaustive
	case ars.TypeDeviceRegistration:
		ack, err := ars.NewAck(0)
		if err != nil {
			return
		}
		buf, err := ack.AppendBinary(nil)
		if err != nil {
			slog.Error("Locator: cannot build an ARS ack", "error", err)
			return
		}
		l.queue(k, ln, ip.PortARS, buf)
		l.startTracking(k, ln)
	case ars.TypeDeviceDeregistration:
		l.stopTracking(k, ln)
	}
}

func (l *Locator) startTracking(k linkKey, ln *link) {
	s, ok, err := models.FindPositionSetting(l.db, k.radio)
	if err != nil {
		slog.Error("Locator: cannot read a position setting", "radio", k.radio, "error", err)
		return
	}
	if !ok || !s.Tracking {
		return
	}
	l.reqID++
	var rid [4]byte
	binary.BigEndian.PutUint32(rid[:], l.reqID)
	l.lrrp = lrrp.Message{Document: lrrp.DocumentTriggeredRequest}
	r := &l.lrrp.Request
	r.RequestID, r.HasRequestID = rid[:], true
	r.Periodic = true
	r.Interval, r.HasInterval = uint32(s.IntervalSeconds), true //nolint:gosec
	r.AccuracyAndTime, r.Speed, r.Direction = true, true, true
	buf, err := l.lrrp.AppendBinary(nil)
	if err != nil {
		slog.Error("Locator: cannot build an LRRP request", "error", err)
		return
	}
	l.queue(k, ln, ip.PortLRRP, buf)
	slot := uint8(1)
	if ln.slot {
		slot = 2
	}
	row := models.LocationRequest{
		RadioID: k.radio, ServiceID: k.service, RequestID: rid[:], IntervalSeconds: s.IntervalSeconds,
		SentAt: time.Now(), RepeaterID: ln.repeater, Slot: slot,
	}
	if err := l.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		slog.Error("Locator: cannot store a location request", "radio", k.radio, "error", err)
	}
}

// StopTracking sends a stop request to radioID for its open location
// request, on the repeater and slot where the request went.
func (l *Locator) StopTracking(radioID uint) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var rows []models.LocationRequest
	if err := l.db.Where("radio_id = ?", radioID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return
	}
	r := rows[0]
	k := linkKey{radio: radioID, service: r.ServiceID}
	ln, ok := l.links[k]
	if !ok {
		rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(r.ServiceID), Respond: true, Source: layer2.SyncSourceBS, Lost: networkLost}) //nolint:gosec
		if err != nil {
			return
		}
		ln = &link{rx: rx, repeater: r.RepeaterID, slot: r.Slot == 2, cc: l.colorCode(r.RepeaterID), last: time.Now()}
		l.links[k] = ln
	}
	l.stopTracking(k, ln)
}

func (l *Locator) stopTracking(k linkKey, ln *link) {
	var rows []models.LocationRequest
	if err := l.db.Where("radio_id = ?", k.radio).Limit(1).Find(&rows).Error; err != nil {
		slog.Error("Locator: cannot read a location request", "radio", k.radio, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	l.lrrp = lrrp.Message{Document: lrrp.DocumentStopRequest}
	l.lrrp.Request.RequestID, l.lrrp.Request.HasRequestID = rows[0].RequestID, true
	buf, err := l.lrrp.AppendBinary(nil)
	if err != nil {
		slog.Error("Locator: cannot build an LRRP stop request", "error", err)
		return
	}
	l.queue(k, ln, ip.PortLRRP, buf)
	if err := l.db.Where("radio_id = ?", k.radio).Delete(&models.LocationRequest{}).Error; err != nil {
		slog.Error("Locator: cannot delete a location request", "radio", k.radio, "error", err)
	}
}

func (l *Locator) queue(k linkKey, ln *link, port ip.Port, payload []byte) {
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, uint32(k.service)), //nolint:gosec
		Destination:     ip.RadioAddress(ip.DefaultNetwork, uint32(k.radio)),   //nolint:gosec
		SourcePort:      port,
		DestinationPort: port,
		TTL:             64,
		Payload:         payload,
	}
	packet, err := ip.AppendIPv4(nil, &d)
	if err != nil {
		slog.Error("Locator: cannot build a datagram", "error", err)
		return
	}
	var h pdu.DataHeader
	err = h.SetUnconfirmed(pdu.UnconfirmedDataHeader{
		SAP:             enums.SAPIPBasedPacketData,
		LLIDDestination: uint32(k.radio),   //nolint:gosec
		LLIDSource:      uint32(k.service), //nolint:gosec
		FullMessageFlag: true,
	})
	if err != nil {
		slog.Error("Locator: cannot build a data header", "error", err)
		return
	}
	ln.queue = append(ln.queue, data.Message{Header: h, Rate: enums.DataTypeRate12, Payload: packet})
	l.pump(k, ln, l.now())
}

func (l *Locator) pump(k linkKey, ln *link, now time.Duration) {
	if ln.tx == nil {
		slot := enums.Slot1
		if ln.slot {
			slot = enums.Slot2
		}
		tx, err := data.NewSender(&data.SenderConfig{ColorCode: ln.cc, Source: layer2.SyncSourceBS, Slot: slot})
		if err != nil {
			slog.Error("Locator: cannot make a data sender", "error", err)
			return
		}
		ln.tx = tx
	}
	if ln.tx.State() == data.SenderIdle && len(ln.queue) > 0 {
		m := ln.queue[0]
		ln.queue = ln.queue[1:]
		if err := ln.tx.Send(now, &m); err != nil {
			slog.Error("Locator: cannot send a message", "radio", k.radio, "error", err)
		}
	}
	l.outs = ln.tx.Tick(now, l.outs[:0])
	for i := range l.outs {
		if o := &l.outs[i]; o.Kind == data.OutputBurst {
			l.respond(k, ln, o.Burst, o.Due-now)
		}
	}
}

func (l *Locator) askSilent() {
	var rows []models.LocationRequest
	if err := l.db.Find(&rows).Error; err != nil {
		slog.Error("Locator: cannot list location requests", "error", err)
		return
	}
	for _, r := range rows {
		interval := maxInterval
		if r.IntervalSeconds < uint(maxInterval/time.Second) {
			interval = time.Duration(r.IntervalSeconds) * time.Second //nolint:gosec
		}
		h, ok := l.heard[r.RadioID]
		if !ok || l.retried[r.RadioID] || !h.at.After(r.SentAt.Add(interval)) {
			continue
		}
		silence := interval * silentAfter
		last := r.SentAt
		var newest []models.Position
		if err := l.db.Where("radio_id = ?", r.RadioID).Order("received_at DESC").Limit(1).Find(&newest).Error; err != nil {
			slog.Error("Locator: cannot read the last position", "radio", r.RadioID, "error", err)
			continue
		}
		if len(newest) == 1 && newest[0].ReceivedAt.After(last) {
			last = newest[0].ReceivedAt
		}
		if time.Since(last) < silence {
			continue
		}
		k := linkKey{radio: r.RadioID, service: r.ServiceID}
		ln, ok := l.links[k]
		if !ok {
			ln = &link{cc: l.colorCode(h.repeater)}
			rx, err := data.NewReceiver(&data.ReceiverConfig{Address: uint32(r.ServiceID), Respond: true, Source: layer2.SyncSourceBS, Lost: networkLost}) //nolint:gosec
			if err != nil {
				continue
			}
			ln.rx = rx
			l.links[k] = ln
		}
		ln.repeater, ln.slot, ln.last = h.repeater, h.slot, time.Now()
		l.retried[r.RadioID] = true
		slog.Info("Locator: asking a silent radio for its position again", "radio", r.RadioID, "repeater", h.repeater)
		l.startTracking(k, ln)
	}
}

func (l *Locator) colorCode(repeater uint) enums.ColorCode {
	var cc []uint
	if err := l.db.Model(&models.Repeater{}).Where("id = ?", repeater).Limit(1).Pluck("color_code", &cc).Error; err != nil || len(cc) == 0 {
		return 1
	}
	return enums.ColorCode(cc[0] & 0xF) //nolint:gosec
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
	if len(l.heard) > 0 && time.Since(l.checked) >= recheckEvery {
		l.checked = time.Now()
		for radio, h := range l.heard {
			if time.Since(h.at) > heardExpiry {
				delete(l.heard, radio)
			}
		}
		l.askSilent()
	}
	for k, ln := range l.links {
		if at, ok := ln.rx.Deadline(); ok && now >= at {
			l.events = ln.rx.Tick(now, l.events[:0])
			l.apply(k, ln, now)
		}
		if ln.tx != nil {
			if at, ok := ln.tx.Deadline(); (ok && now >= at) || (ln.tx.State() == data.SenderIdle && len(ln.queue) > 0) {
				l.pump(k, ln, now)
			}
		}
		if time.Since(ln.last) > linkIdle && len(ln.queue) == 0 && (ln.tx == nil || ln.tx.State() == data.SenderIdle) {
			delete(l.links, k)
		}
	}
}
