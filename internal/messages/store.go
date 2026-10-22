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

// Package messages decodes and stores every data message that the hub
// routes, whatever the SMS Storage setting of its users.
package messages

import (
	"bytes"
	"errors"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	queueDepth     = 4096
	retryWindow    = 30 * time.Second
	responseWindow = 10 * time.Second
	fragmentWindow = time.Minute
	headerOctets   = 12
)

// Store is a calltracker.DataObserver that writes each data message to the
// messages table. DataPacket and DataResponse copy what they need and
// queue it, so they never wait for the database; when the queue is full
// the message is dropped and counted.
type Store struct {
	db      *gorm.DB
	in      chan job
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	dropped atomic.Uint64
	metrics *metrics.Metrics
	notify  atomic.Pointer[notifyBox]

	recent  []recent
	pending []pendingResponse
}

type notifyBox struct{ fn func(*models.Message) }

type job struct {
	response bool
	at       time.Time
	callID   uint
	repeater uint
	slot     uint8
	server   string
	stream   uint
	call     data.Call
	header   pdu.DataHeader
	payload  []byte
	badCRC   bool
	frag     bool
	whole    bool
	resp     packet.Response
	rowSrc   uint
	rowDst   uint
	flush    chan struct{}
}

type recent struct {
	id       uint
	callID   uint
	src, dst uint
	group    bool
	sap      int
	ns       int
	fsn      int
	hash     uint64
	octets   int
	badCRC   bool
	conf     bool
	at       time.Time
}

type pendingResponse struct {
	src, dst uint
	resp     models.CallResponse
	at       time.Time
}

// NewStore starts a Store that writes to db.
func NewStore(db *gorm.DB) *Store {
	s := &Store{
		db:      db,
		in:      make(chan job, queueDepth),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		metrics: metrics.NewMetrics(),
	}
	go s.run()
	return s
}

// SetNotify sets fn to be called, on the goroutine of the Store, with each
// message that the Store creates or changes.
func (s *Store) SetNotify(fn func(*models.Message)) {
	s.notify.Store(&notifyBox{fn: fn})
}

// Stop saves the queued messages and stops the Store.
func (s *Store) Stop() {
	s.once.Do(func() {
		close(s.stop)
		<-s.done
	})
}

// Dropped returns the number of messages that the Store dropped because its
// queue was full.
func (s *Store) Dropped() uint64 {
	return s.dropped.Load()
}

// Flush waits until the Store handled every message queued before it.
func (s *Store) Flush() {
	ch := make(chan struct{})
	select {
	case s.in <- job{flush: ch}:
	case <-s.done:
		return
	}
	select {
	case <-ch:
	case <-s.done:
	}
}

// DataPacket queues the message of ev, which the data call row call
// carried, to be stored.
func (s *Store) DataPacket(call *models.Call, ev *data.Event, at time.Time) {
	if call == nil || ev == nil {
		return
	}
	j := s.jobOf(call, ev, at)
	j.payload = bytes.Clone(ev.Message.Payload)
	j.badCRC = ev.Message.BadCRC
	j.frag = ev.Message.Fragmented
	j.whole = ev.Kind == data.EventReassembled
	s.enqueue(&j)
}

// DataResponse queues the response of ev, to be set on the message that it
// answers.
func (s *Store) DataResponse(call *models.Call, ev *data.Event, at time.Time) {
	if call == nil || ev == nil {
		return
	}
	j := s.jobOf(call, ev, at)
	j.response = true
	j.resp = ev.Response
	j.rowSrc, j.rowDst = call.SourceID, call.DestinationID
	s.enqueue(&j)
}

func (s *Store) jobOf(call *models.Call, ev *data.Event, at time.Time) job {
	slot := uint8(1)
	if call.TimeSlot {
		slot = 2
	}
	return job{
		at:       at,
		callID:   call.ID,
		repeater: call.IngressID,
		slot:     slot,
		server:   call.Transport,
		stream:   call.StreamID,
		call:     ev.Call,
		header:   ev.Message.Header,
	}
}

func (s *Store) enqueue(j *job) {
	select {
	case <-s.stop:
		return
	default:
	}
	select {
	case s.in <- *j:
	default:
		s.metrics.IncrementMessagesDropped()
		n := s.dropped.Add(1)
		if n&(n-1) == 0 {
			slog.Error("Message store: queue full, data messages not stored", "dropped", n)
		}
	}
}

func (s *Store) run() {
	defer close(s.done)
	for {
		select {
		case j := <-s.in:
			s.handle(&j)
		case <-s.stop:
			for {
				select {
				case j := <-s.in:
					s.handle(&j)
				default:
					return
				}
			}
		}
	}
}

func (s *Store) handle(j *job) {
	if j.flush != nil {
		close(j.flush)
		return
	}
	s.prune(time.Now())
	if j.response {
		s.response(j)
		return
	}
	if j.frag {
		return
	}
	s.packet(j)
}

func (s *Store) prune(now time.Time) {
	keep := s.recent[:0]
	for _, r := range s.recent {
		if now.Sub(r.at) < fragmentWindow {
			keep = append(keep, r)
		}
	}
	s.recent = keep
	pend := s.pending[:0]
	for _, p := range s.pending {
		if now.Sub(p.at) < responseWindow {
			pend = append(pend, p)
		}
	}
	s.pending = pend
}

func (s *Store) packet(j *job) {
	msg := s.decode(j)
	r := recent{
		callID: j.callID,
		src:    msg.SourceID,
		dst:    msg.DestinationID,
		group:  msg.IsGroup,
		sap:    optional(msg.SAP),
		ns:     optional(msg.SendSequence),
		fsn:    optional(msg.FragmentSeq),
		hash:   hashOf(j.payload),
		octets: len(j.payload),
		badCRC: msg.BadCRC,
		conf:   msg.Confirmed,
		at:     j.at,
	}

	if j.whole {
		if i := s.firstFragment(&r); i >= 0 {
			s.replace(i, &r, msg)
			return
		}
	} else if i, dup := s.retryOf(&r); i >= 0 {
		if !dup {
			s.replace(i, &r, msg)
		}
		return
	}

	if p := s.takePending(msg); p != nil {
		msg.Response = p.resp
	}
	if err := s.db.Omit(clause.Associations).Create(msg).Error; err != nil {
		slog.Error("Message store: cannot store a data message", "src", msg.SourceID, "dst", msg.DestinationID, "error", err)
		return
	}
	r.id = msg.ID
	s.recent = append(s.recent, r)
	s.changed(msg)
}

func (s *Store) firstFragment(r *recent) int {
	for i := len(s.recent) - 1; i >= 0; i-- {
		e := &s.recent[i]
		if e.src == r.src && e.dst == r.dst && e.group == r.group && e.sap == r.sap && e.fsn == 0 && r.at.Sub(e.at) < fragmentWindow {
			return i
		}
	}
	return -1
}

func (s *Store) retryOf(r *recent) (int, bool) {
	for i := len(s.recent) - 1; i >= 0; i-- {
		e := &s.recent[i]
		if e.src != r.src || e.dst != r.dst || e.group != r.group || e.sap != r.sap {
			continue
		}
		sameCall := e.callID == r.callID && r.callID != 0
		retry := r.ns >= 0 && e.ns == r.ns && r.at.Sub(e.at) < retryWindow
		if !sameCall && !retry {
			continue
		}
		if e.hash == r.hash {
			return i, true
		}
		if retry && e.badCRC && !r.badCRC && e.octets == r.octets {
			return i, false
		}
	}
	return -1, false
}

func (s *Store) replace(i int, r *recent, msg *models.Message) {
	e := &s.recent[i]
	var row models.Message
	if err := s.db.First(&row, e.id).Error; err != nil {
		slog.Error("Message store: cannot load a message to replace", "id", e.id, "error", err)
		return
	}
	row.ReceivedAt = msg.ReceivedAt
	row.TransmissionID = msg.TransmissionID
	row.RepeaterID, row.Slot, row.Transport, row.StreamID = msg.RepeaterID, msg.Slot, msg.Transport, msg.StreamID
	row.Format, row.Text, row.ChecksumOK = msg.Format, msg.Text, msg.ChecksumOK
	row.BadCRC, row.Blocks, row.Octets, row.Raw = msg.BadCRC, msg.Blocks, msg.Octets, msg.Raw
	row.Confirmed = row.Confirmed || msg.Confirmed
	if row.SendSequence == nil {
		row.SendSequence = msg.SendSequence
	}
	if err := s.db.Omit(clause.Associations).Save(&row).Error; err != nil {
		slog.Error("Message store: cannot replace a message", "id", row.ID, "error", err)
		return
	}
	id := e.id
	*e = *r
	e.id = id
	if r.fsn < 0 {
		e.fsn = -1
	}
	s.changed(&row)
}

func (s *Store) response(j *job) {
	resp := responseOf(j.resp)
	if resp == models.CallResponseNone {
		return
	}
	src, dst := j.rowDst, j.rowSrc
	if s.answer(j, src, dst, resp) {
		return
	}
	s.pending = append(s.pending, pendingResponse{src: src, dst: dst, resp: resp, at: j.at})
}

func (s *Store) answer(j *job, src, dst uint, resp models.CallResponse) bool {
	for i := len(s.recent) - 1; i >= 0; i-- {
		e := &s.recent[i]
		if e.src != src || e.dst != dst || e.group || !e.conf || j.at.Before(e.at) || j.at.Sub(e.at) >= responseWindow {
			continue
		}
		res := s.db.Model(&models.Message{}).Where("id = ?", e.id).Update("response", resp)
		if res.Error != nil {
			slog.Error("Message store: cannot set a response", "id", e.id, "error", res.Error)
			return true
		}
		var row models.Message
		if err := s.db.First(&row, e.id).Error; err == nil {
			s.changed(&row)
		}
		return true
	}
	return false
}

func (s *Store) takePending(msg *models.Message) *pendingResponse {
	for i := len(s.pending) - 1; i >= 0; i-- {
		p := s.pending[i]
		if p.src != msg.SourceID || p.dst != msg.DestinationID || msg.IsGroup || !msg.Confirmed || p.at.Before(msg.ReceivedAt) || p.at.Sub(msg.ReceivedAt) >= responseWindow {
			continue
		}
		s.pending = append(s.pending[:i], s.pending[i+1:]...)
		return &p
	}
	return nil
}

func (s *Store) changed(msg *models.Message) {
	if b := s.notify.Load(); b != nil && b.fn != nil {
		b.fn(msg)
	}
}

func (s *Store) decode(j *job) *models.Message {
	h := &j.header
	msg := &models.Message{
		ReceivedAt:    j.at,
		SourceID:      uint(j.call.Source),
		DestinationID: uint(j.call.Target),
		IsGroup:       j.call.Group,
		RepeaterID:    j.repeater,
		Slot:          j.slot,
		Transport:     j.server,
		StreamID:      j.stream,
		BadCRC:        j.badCRC,
		ChecksumOK:    true,
		Octets:        uint(len(j.payload)),
	}
	if dst, src, ok := h.Addresses(); ok && dst != 0 && src != 0 {
		msg.SourceID, msg.DestinationID = uint(src), uint(dst)
	}
	if j.callID != 0 {
		id := j.callID
		msg.TransmissionID = &id
	}
	if sap, ok := h.SAP(); ok {
		v := uint8(sap)
		msg.SAP = &v
	}
	if n, ok := h.BlocksToFollow(); ok {
		msg.Blocks = uint(n)
	}
	if c, ok := h.AsConfirmed(); ok {
		ns, fsn := c.SendSequenceNumber, c.FragmentSequenceNumber
		msg.Confirmed, msg.SendSequence, msg.FragmentSeq = true, &ns, &fsn
	} else if u, ok := h.AsUnconfirmed(); ok {
		fsn := u.FragmentSequenceNumber
		msg.FragmentSeq = &fsn
	} else if d, ok := h.AsDefinedData(); ok {
		msg.Confirmed = d.ResponseRequested
	} else if r, ok := h.AsRawData(); ok {
		msg.Confirmed = r.ResponseRequested
	}

	enc, err := h.Encode()
	if err == nil {
		msg.Raw = make([]byte, 0, headerOctets+len(j.payload))
		msg.Raw = append(append(msg.Raw, enc[:]...), j.payload...)
	} else {
		msg.Raw = bytes.Clone(j.payload)
	}

	buf, text, err := sms.Decode(nil, h, j.payload)
	switch {
	case err == nil || errors.Is(err, ip.ErrChecksum):
		msg.Format = formatOf(text.Format)
		body := clean(buf)
		msg.Text = &body
		msg.ChecksumOK = err == nil
	case errors.Is(err, sms.ErrNotText):
		msg.Format = models.MessageFormatData
	default:
		msg.Format = models.MessageFormatUnknown
	}
	return msg
}

func clean(b []byte) string {
	s := string(b)
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	return strings.ReplaceAll(s, "\x00", "")
}

func formatOf(f sms.Format) models.MessageFormat {
	switch f {
	case sms.FormatTMS:
		return models.MessageFormatTMS
	case sms.FormatIP:
		return models.MessageFormatIP
	case sms.FormatAnyTone:
		return models.MessageFormatAnyTone
	case sms.FormatDefinedData:
		return models.MessageFormatDefinedData
	case sms.FormatHytera:
		return models.MessageFormatHytera
	}
	return models.MessageFormatUnknown
}

func responseOf(r packet.Response) models.CallResponse {
	switch {
	case r == packet.ResponseACK:
		return models.CallResponseACK
	case r == packet.ResponseSACK:
		return models.CallResponseSACK
	case r.Class() == packet.ResponseNACKIllegalFormat.Class():
		return models.CallResponseNACK
	}
	return models.CallResponseNone
}

func optional(v *uint8) int {
	if v == nil {
		return -1
	}
	return int(*v)
}

func hashOf(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}
