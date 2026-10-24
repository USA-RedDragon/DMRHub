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

// Package locator is a DMRHub plugin that serves ARS and location to
// radios, at the IDs an admin gives it, such as 5050 to 5059 that
// BrandMeister uses. It answers the data that radios send to those IDs, as
// the destination station does, asks the radios whose owners turned
// tracking on for their positions, and gives the hub the positions they
// report.
package locator

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/location"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/ars"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
)

const (
	tickInterval = 50 * time.Millisecond
	recheckEvery = time.Second
	silentAfter  = 3
	heardExpiry  = time.Hour
	maxInterval  = 24 * time.Hour
	linkIdle     = 2 * time.Minute
	networkLost  = 500 * time.Millisecond
	// settle is how long after its last data the Locator leaves a radio
	// alone, so that it does not ask a radio whose registration is still
	// coming in.
	settle = 5 * time.Second
	// stopAgain is how long the Locator waits before it asks a radio that
	// is not tracked to stop reporting again.
	stopAgain = 10 * time.Minute
)

type linkKey struct {
	radio, service uint32
}

type link struct {
	rx    *data.Receiver
	tx    *data.Sender
	queue []data.Message
	slot  uint32
	cc    enums.ColorCode
	last  time.Time
}

// request is the triggered location request that is open with a radio.
type request struct {
	id       []byte
	service  uint32
	interval time.Duration
	sentAt   time.Time
	slot     uint32
	cc       enums.ColorCode
}

// radio is what the Locator knows of one radio.
type radio struct {
	tracked  bool
	interval time.Duration
	heard    time.Time
	slot     uint32
	cc       enums.ColorCode
	req      *request
	// service is the ID the radio last sent data to.
	service uint32
	lastPos time.Time
	retried bool
	stopped time.Time
}

// Locator is the location server. Its methods are safe for concurrent use.
type Locator struct {
	send    func(*pluginv1.Burst) error
	report  func(*pluginv1.Position) error
	service uint32
	epoch   time.Time
	mu      sync.Mutex
	links   map[linkKey]*link
	radios  map[uint32]*radio
	dec     location.Decoder
	events  []data.Event
	outs    []data.Output
	lrrp    lrrp.Message
	reqID   uint32
	stream  uint32
	checked time.Time
	done    chan struct{}
	wg      sync.WaitGroup
}

// New returns a Locator that sends its bursts with send and the positions
// that radios report with report. It asks radios for their positions from
// service, its first ID.
func New(send func(*pluginv1.Burst) error, report func(*pluginv1.Position) error, service uint32) *Locator {
	l := &Locator{
		send:    send,
		report:  report,
		service: service,
		epoch:   time.Now(),
		links:   map[linkKey]*link{},
		radios:  map[uint32]*radio{},
		stream:  0x10C0_0000,
		done:    make(chan struct{}),
	}
	var seed [4]byte
	_, _ = rand.Read(seed[:])
	l.reqID = binary.BigEndian.Uint32(seed[:])
	l.wg.Add(1)
	go l.tick()
	return l
}

// Stop ends the timers of the Locator.
func (l *Locator) Stop() {
	close(l.done)
	l.wg.Wait()
}

// Serve runs a Locator on conn until it ends. Give it to plugin.Run.
func Serve(_ context.Context, conn *plugin.Conn) error {
	ranges := conn.Ranges()
	if len(ranges) == 0 {
		return errNoIDs
	}
	l := New(conn.Send, conn.SendPosition, ranges[0].GetFirst())
	defer l.Stop()
	if err := conn.SubscribeTracking(true); err != nil {
		return fmt.Errorf("cannot subscribe to tracking: %w", err)
	}
	for {
		m, err := conn.Recv()
		if err != nil {
			return fmt.Errorf("cannot receive: %w", err)
		}
		switch msg := m.GetMessage().(type) {
		case *pluginv1.ConnectResponse_Burst:
			l.Handle(msg.Burst)
		case *pluginv1.ConnectResponse_Tracked:
			l.SetTracked(msg.Tracked.GetRadios())
		case *pluginv1.ConnectResponse_Tracking:
			l.SetTracking(msg.Tracking)
		case *pluginv1.ConnectResponse_Heard:
			l.Heard(msg.Heard)
		case *pluginv1.ConnectResponse_Refusal:
			slog.Warn("Locator: the hub refused a burst", "stream", msg.Refusal.GetStreamId(), "reason", msg.Refusal.GetReason())
		case *pluginv1.ConnectResponse_Ranges:
			if r := msg.Ranges.GetRanges(); len(r) > 0 {
				l.setService(r[0].GetFirst())
			}
		case *pluginv1.ConnectResponse_Welcome:
		default:
			return errUnknown
		}
	}
}

var (
	errNoIDs    = errors.New("the plugin has no IDs")
	errUnknown  = errors.New("unknown message from the hub")
	errNoReport = errors.New("no report")
)

func (l *Locator) setService(service uint32) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.service = service
}

func (l *Locator) now() time.Duration {
	return time.Since(l.epoch)
}

func (l *Locator) radio(id uint32) *radio {
	r, ok := l.radios[id]
	if !ok {
		r = &radio{slot: 1, cc: 1}
		l.radios[id] = r
	}
	return r
}

// SetTracked takes all the radios whose owners turned tracking on. A radio
// that was tracked and is not among them is asked to stop reporting.
func (l *Locator) SetTracked(radios []*pluginv1.Tracking) {
	l.mu.Lock()
	defer l.mu.Unlock()
	on := make(map[uint32]*pluginv1.Tracking, len(radios))
	for _, t := range radios {
		if t.GetOn() {
			on[t.GetRadio()] = t
		}
	}
	for id, r := range l.radios {
		if _, ok := on[id]; !ok && r.tracked {
			l.untrack(id, r)
		}
	}
	for _, t := range on {
		l.track(t)
	}
}

// SetTracking takes a change of the tracking of one radio. A radio whose
// tracking was turned off is asked to stop reporting; one with a new
// interval is asked again with it.
func (l *Locator) SetTracking(t *pluginv1.Tracking) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !t.GetOn() {
		if r, ok := l.radios[t.GetRadio()]; ok {
			l.untrack(t.GetRadio(), r)
		}
		return
	}
	l.track(t)
}

func (l *Locator) track(t *pluginv1.Tracking) {
	r := l.radio(t.GetRadio())
	interval := time.Duration(t.GetIntervalSeconds()) * time.Second
	changed := r.tracked && r.interval != interval
	r.tracked, r.interval = true, interval
	if changed && r.req != nil {
		k, ln := l.requestLink(t.GetRadio(), r)
		l.stopTracking(k, ln, r)
		l.startTracking(k, ln, r)
	}
}

func (l *Locator) untrack(id uint32, r *radio) {
	r.tracked = false
	if r.req != nil {
		k, ln := l.requestLink(id, r)
		l.stopTracking(k, ln, r)
	}
}

// Heard takes where a tracked radio was heard, so that the Locator can ask
// it for its position when it has not reported.
func (l *Locator) Heard(h *pluginv1.Heard) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.radio(h.GetRadio())
	r.heard, r.slot, r.cc = time.Now(), h.GetSlot(), enums.ColorCode(h.GetColorCode()&0xF) //nolint:gosec // masked to 4 bits
}

// Handle takes a burst to one of the Locator's IDs.
func (l *Locator) Handle(b *pluginv1.Burst) {
	if b.GetGroupCall() || b.GetFrameType() != pluginv1.FrameType_FRAME_TYPE_DATA_SYNC || len(b.GetPayload()) != len(layer2.RawBurst{}) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	k := linkKey{radio: b.GetSource(), service: b.GetDestination()}
	ln, ok := l.links[k]
	if !ok {
		var err error
		if ln, err = newLink(k.service); err != nil {
			slog.Error("Locator: cannot make a data receiver", "error", err)
			return
		}
		l.links[k] = ln
	}
	raw := layer2.RawBurst(b.GetPayload())
	ln.slot, ln.last, ln.cc = b.GetSlot(), time.Now(), raw.SlotType().ColorCode
	l.radio(k.radio).service = k.service
	now := l.now()
	d := plugin.DataOf(b)
	l.events = ln.rx.Burst(now, &raw, d.Hint(now), l.events[:0])
	l.apply(k, ln, now)
}

func newLink(service uint32) (*link, error) {
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: service, Respond: true, Source: layer2.SyncSourceBS, Lost: networkLost})
	if err != nil {
		return nil, fmt.Errorf("cannot make a data receiver: %w", err)
	}
	return &link{rx: rx, slot: 1, cc: 1, last: time.Now()}, nil
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
	b := &pluginv1.Burst{
		Source: k.service, Destination: k.radio, Slot: ln.slot, StreamId: l.stream,
		FrameType: pluginv1.FrameType_FRAME_TYPE_DATA_SYNC, DataTypeOrVoiceSequence: uint32(raw.SlotType().DataType),
		Payload: append([]byte(nil), raw[:]...),
	}
	if b.Slot != 2 {
		b.Slot = 1
	}
	send := func() {
		if err := l.send(b); err != nil {
			slog.Warn("Locator: cannot send a burst", "radio", k.radio, "error", err)
		}
	}
	if wait <= 0 {
		send()
		return
	}
	time.AfterFunc(wait, send)
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
	r := l.radio(k.radio)
	if f.RequestIDLength > 0 {
		l.learnRequest(k, ln, r, f.RequestID[:f.RequestIDLength])
	}
	if err != nil {
		if !errors.Is(err, location.ErrNoFix) && !errors.Is(err, location.ErrNoPosition) {
			slog.Debug("Locator: a report does not decode", "radio", k.radio, "error", err)
		}
		return
	}
	if !r.tracked {
		return
	}
	pos, err := positionOf(&f, k, ln)
	if err != nil {
		return
	}
	r.lastPos, r.retried = time.Now(), false
	if err := l.report(pos); err != nil {
		slog.Warn("Locator: cannot report a position", "radio", k.radio, "error", err)
	}
}

// learnRequest notes the request id that a report of the radio r answers.
// A tracked radio's request is then known even after a restart; a radio
// that is not tracked is asked to stop.
func (l *Locator) learnRequest(k linkKey, ln *link, r *radio, id []byte) {
	if r.tracked {
		if r.req == nil {
			r.req = &request{id: append([]byte(nil), id...), service: k.service, interval: r.interval, sentAt: time.Now(), slot: ln.slot, cc: ln.cc}
		}
		return
	}
	if time.Since(r.stopped) < stopAgain {
		return
	}
	r.stopped = time.Now()
	slog.Info("Locator: asking a radio that is not tracked to stop reporting", "radio", k.radio)
	l.sendStop(k, ln, id)
}

func (l *Locator) registration(k linkKey, ln *link, payload []byte) {
	m, err := ars.DecodeMessage(payload)
	if err != nil {
		slog.Debug("Locator: an ARS message does not decode", "radio", k.radio, "error", err)
		return
	}
	r := l.radio(k.radio)
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
		if r.tracked {
			l.startTracking(k, ln, r)
		}
	case ars.TypeDeviceDeregistration:
		l.stopTracking(k, ln, r)
	}
}

func (l *Locator) startTracking(k linkKey, ln *link, r *radio) {
	l.reqID++
	var rid [4]byte
	binary.BigEndian.PutUint32(rid[:], l.reqID)
	l.lrrp = lrrp.Message{Document: lrrp.DocumentTriggeredRequest}
	q := &l.lrrp.Request
	q.RequestID, q.HasRequestID = rid[:], true
	q.Periodic = true
	q.Interval, q.HasInterval = uint32(r.interval/time.Second), true //nolint:gosec // at most an hour
	q.AccuracyAndTime, q.Speed, q.Direction = true, true, true
	buf, err := l.lrrp.AppendBinary(nil)
	if err != nil {
		slog.Error("Locator: cannot build an LRRP request", "error", err)
		return
	}
	l.queue(k, ln, ip.PortLRRP, buf)
	r.req = &request{id: rid[:], service: k.service, interval: r.interval, sentAt: time.Now(), slot: ln.slot, cc: ln.cc}
}

func (l *Locator) stopTracking(k linkKey, ln *link, r *radio) {
	if r.req == nil {
		return
	}
	l.sendStop(k, ln, r.req.id)
	r.req = nil
}

func (l *Locator) sendStop(k linkKey, ln *link, id []byte) {
	l.lrrp = lrrp.Message{Document: lrrp.DocumentStopRequest}
	l.lrrp.Request.RequestID, l.lrrp.Request.HasRequestID = id, true
	buf, err := l.lrrp.AppendBinary(nil)
	if err != nil {
		slog.Error("Locator: cannot build an LRRP stop request", "error", err)
		return
	}
	l.queue(k, ln, ip.PortLRRP, buf)
}

// requestLink returns the link of the open request of the radio r, on the
// slot and color code where the request went.
func (l *Locator) requestLink(id uint32, r *radio) (linkKey, *link) {
	k := linkKey{radio: id, service: r.req.service}
	if ln, ok := l.links[k]; ok {
		return k, ln
	}
	ln, err := newLink(k.service)
	if err != nil {
		slog.Error("Locator: cannot make a data receiver", "error", err)
		ln = &link{slot: 1, cc: 1}
	}
	ln.slot, ln.cc = r.req.slot, r.req.cc
	l.links[k] = ln
	return k, ln
}

func (l *Locator) queue(k linkKey, ln *link, port ip.Port, payload []byte) {
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, k.service),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, k.radio),
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
		LLIDDestination: k.radio,
		LLIDSource:      k.service,
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
		if ln.slot == 2 {
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

// askHeard asks each tracked radio that was heard for its position: one
// with no open request and no data in the last settle once, and one whose
// request went silent for silentAfter intervals once until it reports. It
// asks from the ID of the open request, else from the ID the radio last
// sent data to, else from the Locator's first ID.
func (l *Locator) askHeard() {
	for id, r := range l.radios {
		if !r.tracked || r.retried || r.heard.IsZero() || time.Since(r.heard) > heardExpiry || (r.req == nil && l.busy(id)) {
			continue
		}
		if r.req != nil {
			interval := min(max(r.req.interval, time.Second), maxInterval)
			if !r.heard.After(r.req.sentAt.Add(interval)) {
				continue
			}
			last := r.req.sentAt
			if r.lastPos.After(last) {
				last = r.lastPos
			}
			if time.Since(last) < interval*silentAfter {
				continue
			}
		}
		service := l.service
		switch {
		case r.req != nil:
			service = r.req.service
		case r.service != 0:
			service = r.service
		}
		k := linkKey{radio: id, service: service}
		ln, ok := l.links[k]
		if !ok {
			var err error
			if ln, err = newLink(service); err != nil {
				continue
			}
			l.links[k] = ln
		}
		ln.slot, ln.cc, ln.last = r.slot, r.cc, time.Now()
		r.retried = true
		slog.Info("Locator: asking a tracked radio for its position", "radio", id)
		l.stopTracking(k, ln, r)
		l.startTracking(k, ln, r)
	}
}

// busy reports whether the radio sent or got data in the last settle.
func (l *Locator) busy(radio uint32) bool {
	for k, ln := range l.links {
		if k.radio == radio && time.Since(ln.last) < settle {
			return true
		}
	}
	return false
}

func positionOf(f *location.Fix, k linkKey, ln *link) (*pluginv1.Position, error) {
	if !f.Valid || math.IsNaN(f.Latitude) || math.IsNaN(f.Longitude) ||
		f.Latitude < -90 || f.Latitude > 90 || f.Longitude < -180 || f.Longitude > 180 ||
		(f.Latitude == 0 && f.Longitude == 0) {
		return nil, errNoReport
	}
	p := &pluginv1.Position{Radio: k.radio, Service: k.service, Slot: ln.slot, Latitude: f.Latitude, Longitude: f.Longitude}
	switch f.Format {
	case location.FormatLRRP:
		p.Format = pluginv1.PositionFormat_POSITION_FORMAT_LRRP
	case location.FormatNMEA:
		p.Format = pluginv1.PositionFormat_POSITION_FORMAT_NMEA
	case location.FormatHDAP:
		p.Format = pluginv1.PositionFormat_POSITION_FORMAT_HDAP
	default:
		return nil, errNoReport
	}
	if f.HasRadius {
		p.AccuracyMeters = &f.Radius
	}
	if f.HasSpeed {
		p.SpeedMetersPerSecond = &f.Speed
	}
	if f.HasCourse {
		p.HeadingDegrees = &f.Course
	}
	if f.HasAltitude {
		p.AltitudeMeters = &f.Altitude
	}
	if f.HasTime {
		at := time.Date(int(f.Year), time.Month(f.Month), int(f.Day), int(f.Hour), int(f.Minute), int(f.Second), 0, time.UTC).Unix()
		p.FixTime = &at
	}
	return p, nil
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
	if time.Since(l.checked) >= recheckEvery {
		l.checked = time.Now()
		l.askHeard()
	}
	for k, ln := range l.links {
		if ln.rx != nil {
			if at, ok := ln.rx.Deadline(); ok && now >= at {
				l.events = ln.rx.Tick(now, l.events[:0])
				l.apply(k, ln, now)
			}
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
