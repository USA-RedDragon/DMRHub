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

package storeforward

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// AnswerWait is how long a confirmed text waits for its ACK before the
	// hub holds it.
	AnswerWait   = 10 * time.Second
	answerWindow = 2 * time.Minute
	// SweepEvery is the time between two sweeps.
	SweepEvery = 5 * time.Second
	// IdleGap is how long a radio must be quiet before the hub sends it
	// its held texts, so that it has stopped sending and listens.
	IdleGap      = 2 * time.Second
	claimTimeout = 5 * time.Minute
	maxAttempts  = 5
	queueDepth   = 1024
)

const (
	colState     = "state"
	colClaimedBy = "claimed_by"
	colDoneAt    = "done_at"
	colAttempts  = "attempts"
)

// TextFormats returns the formats of the messages that store and forward
// holds: the text formats.
func TextFormats() []models.MessageFormat {
	return []models.MessageFormat{
		models.MessageFormatTMS, models.MessageFormatIP, models.MessageFormatAnyTone,
		models.MessageFormatDefinedData, models.MessageFormatHytera,
	}
}

// Sender sends data to a radio for the Service.
type Sender interface {
	// SendData sends m as a stream of bursts to the repeater on the slot
	// (true for timeslot 2), and returns how it ended once the message is
	// sent, acknowledged or given up.
	SendData(ctx context.Context, repeater uint, slot bool, m data.Message) (data.Result, error)
	// LastBurst returns when this replica last got a burst from radio.
	LastBurst(radio uint) (time.Time, bool)
}

type notRouted struct {
	dst uint
	at  time.Time
}

type recentText struct {
	m  *models.Message
	at time.Time
}

type job struct {
	stored          *models.Message
	radio, repeater uint
	slot            bool
	flush           chan struct{}
}

// Service holds the texts that the hub cannot deliver to the radios of
// users who turned store and forward on, and sends them when the radio is
// heard again. The held texts are rows in the database, which the replicas
// of the hub share; a replica claims the texts of a radio in the database
// before it sends them, so one replica sends each.
type Service struct {
	db      *gorm.DB
	cfg     config.Source
	clock   clock.Clock
	replica string

	sender   Sender
	senderMu sync.RWMutex
	jobs     chan job
	flush    func()

	mu        sync.Mutex
	sending   map[uint]bool
	notRouted map[uint]notRouted
	recent    map[uint]recentText

	quit       chan struct{}
	wg         sync.WaitGroup
	deliveries sync.WaitGroup
	started    atomic.Bool
	once       sync.Once
}

// Option sets an option of a Service.
type Option func(*Service)

// WithClock makes the Service keep time with c.
func WithClock(c clock.Clock) Option {
	return func(s *Service) { s.clock = c }
}

// WithFlush makes the Service call flush before it reads the text format
// of a radio, so that the last text of the radio is in the database: flush
// waits until the message store stored the messages that the hub routed
// before it.
func WithFlush(flush func()) Option {
	return func(s *Service) { s.flush = flush }
}

// New returns a Service of the hub replica named replica that keeps its rows in db
// and reads its limits from cfg. Start starts it.
func New(db *gorm.DB, cfg config.Source, replica string, opts ...Option) *Service {
	s := &Service{
		db: db, cfg: cfg, clock: clock.Real(), replica: replica,
		notRouted: map[uint]notRouted{},
		recent:    map[uint]recentText{},
		jobs:      make(chan job, queueDepth),
		sending:   map[uint]bool{},
		quit:      make(chan struct{}),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// SetSender sets what sends the held texts.
func (s *Service) SetSender(sender Sender) {
	s.senderMu.Lock()
	defer s.senderMu.Unlock()
	s.sender = sender
}

func (s *Service) getSender() Sender {
	s.senderMu.RLock()
	defer s.senderMu.RUnlock()
	return s.sender
}

// Start starts the work of the Service: the handling of stored texts and
// sightings, and a sweep every SweepEvery.
func (s *Service) Start() {
	s.started.Store(true)
	s.wg.Add(1)
	go s.run()
}

// Stop stops the Service and waits for its work to end.
func (s *Service) Stop() {
	s.once.Do(func() {
		close(s.quit)
		s.wg.Wait()
	})
}

// Flush waits until the Service handled the texts and sightings queued
// before it, and for the deliveries that they started.
func (s *Service) Flush() {
	if !s.started.Load() {
		s.deliveries.Wait()
		return
	}
	ch := make(chan struct{})
	select {
	case s.jobs <- job{flush: ch}:
		select {
		case <-ch:
		case <-s.quit:
		}
	case <-s.quit:
	}
	s.deliveries.Wait()
}

func (s *Service) run() {
	defer s.wg.Done()
	ticker := s.clock.NewTicker(SweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.quit:
			return
		case j := <-s.jobs:
			switch {
			case j.flush != nil:
				close(j.flush)
			case j.stored != nil:
				s.Hold(j.stored, s.clock.Now(), false)
			default:
				s.Heard(j.radio, j.repeater, j.slot)
			}
		case now := <-ticker.C():
			s.Sweep(now)
		}
	}
}

// NotRouted notes that the hub found no repeater for the private data
// stream with the ID stream to the radio dst, so that its message is held
// whether the message store stored it before or after. It never blocks.
func (s *Service) NotRouted(stream, dst uint) {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.notRouted[stream]; ok && r.dst == dst {
		return
	}
	s.notRouted[stream] = notRouted{dst: dst, at: now}
	if r, ok := s.recent[stream]; ok && r.m.DestinationID == dst {
		delete(s.recent, stream)
		s.queueHold(r.m)
	}
}

// Stored notes the message m, which the message store created or changed,
// so that it is held when the hub did not route its stream. It never
// blocks.
func (s *Service) Stored(m *models.Message) {
	if m == nil || m.IsGroup || m.Text == nil || m.ID == 0 {
		return
	}
	c := *m
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.notRouted[m.StreamID]; ok && r.dst == m.DestinationID {
		s.queueHold(&c)
		return
	}
	s.recent[m.StreamID] = recentText{m: &c, at: now}
}

func (s *Service) queueHold(m *models.Message) {
	select {
	case s.jobs <- job{stored: m}:
	default:
		slog.Warn("Store and forward: queue full, a text was not held", "src", m.SourceID, "dst", m.DestinationID)
	}
}

// QueueHeard queues a sighting of radio on the repeater and slot, for
// Heard. It never blocks.
func (s *Service) QueueHeard(radio, repeater uint, slot bool) {
	select {
	case s.jobs <- job{radio: radio, repeater: repeater, slot: slot}:
	default:
		slog.Warn("Store and forward: queue full, a sighting was dropped", "radio", radio)
	}
}

func isText(f models.MessageFormat) bool {
	for _, t := range TextFormats() {
		if f == t {
			return true
		}
	}
	return false
}

// Hold keeps the text m for its radio when the user who owns the radio
// turned store and forward on and has room for it. unanswered marks a
// confirmed text without an ACK. It reports whether it held m.
func (s *Service) Hold(m *models.Message, now time.Time, unanswered bool) bool {
	if m.IsGroup || m.Text == nil || !isText(m.Format) || m.BadCRC || m.SourceID == m.DestinationID {
		return false
	}
	owner, ok, err := models.RadioIDOwner(s.db, m.DestinationID)
	if err != nil || !ok {
		if err != nil {
			slog.Error("Store and forward: cannot find the owner of a radio", "radio", m.DestinationID, "error", err)
		}
		return false
	}
	var user models.User
	if err := s.db.Select("id", "store_forward", "approved", "suspended").First(&user, owner).Error; err != nil {
		slog.Error("Store and forward: cannot read a user", "user", owner, "error", err)
		return false
	}
	if !user.StoreForward || !user.Approved || user.Suspended {
		return false
	}
	var held int64
	if err := s.db.Model(&models.HeldMessage{}).Where("user_id = ? AND state IN ?", owner,
		[]models.HeldState{models.HeldStateHeld, models.HeldStateSending}).Count(&held).Error; err != nil {
		slog.Error("Store and forward: cannot count the held texts", "user", owner, "error", err)
		return false
	}
	if held >= int64(s.limits().MostHeld()) {
		slog.Info("Store and forward: the user has the most held texts, a text was not held", "user", owner, "radio", m.DestinationID)
		return false
	}
	row := models.HeldMessage{MessageID: m.ID, RadioID: m.DestinationID, UserID: owner, State: models.HeldStateHeld, HeldAt: now}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		slog.Error("Store and forward: cannot hold a text", "message", m.ID, "error", res.Error)
		return false
	}
	if res.RowsAffected == 0 {
		return false
	}
	slog.Info("Store and forward: holding a text for a radio", "message", m.ID, "radio", m.DestinationID, "src", m.SourceID, "unanswered", unanswered)
	return true
}

func (s *Service) limits() config.Messages {
	if s.cfg == nil {
		return config.Messages{}
	}
	if c := s.cfg.Get(); c != nil {
		return c.DMR.Messages
	}
	return config.Messages{}
}

// Sweep holds the confirmed texts that got no answer in AnswerWait, ends
// the held texts older than the hold limit, gives back the claims of a
// replica that stopped, and forgets old streams.
func (s *Service) Sweep(now time.Time) {
	var msgs []models.Message
	err := s.db.Where("is_group = ? AND confirmed = ? AND (response IS NULL OR response = ?) AND format IN ?",
		false, true, models.CallResponseNone, TextFormats()).
		Where("received_at > ? AND received_at <= ?", now.Add(-answerWindow).UTC(), now.Add(-AnswerWait).UTC()).
		Where("NOT EXISTS (SELECT 1 FROM held_messages WHERE held_messages.message_id = messages.id)").
		Order("id").Limit(queueDepth).Find(&msgs).Error
	if err != nil {
		slog.Error("Store and forward: cannot find unanswered texts", "error", err)
	}
	for i := range msgs {
		s.Hold(&msgs[i], now, true)
	}
	if err := s.db.Model(&models.HeldMessage{}).Where("state = ? AND held_at < ?", models.HeldStateHeld, now.Add(-s.limits().HoldFor()).UTC()).
		Updates(map[string]any{colState: models.HeldStateExpired, colDoneAt: now.UTC()}).Error; err != nil {
		slog.Error("Store and forward: cannot end old held texts", "error", err)
	}
	if err := s.db.Model(&models.HeldMessage{}).Where("state = ? AND claimed_at < ?", models.HeldStateSending, now.Add(-claimTimeout).UTC()).
		Updates(map[string]any{colState: models.HeldStateHeld, colClaimedBy: ""}).Error; err != nil {
		slog.Error("Store and forward: cannot give back old claims", "error", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.notRouted {
		if now.Sub(v.at) > answerWindow {
			delete(s.notRouted, k)
		}
	}
	for k, r := range s.recent {
		if now.Sub(r.at) > answerWindow {
			delete(s.recent, k)
		}
	}
}

// RadioFormat returns the text format of the radio, from the last private
// text that it sent through a repeater that its owner owns or an admin
// trusts, so that a hotspot that sends with the ID of the radio cannot set
// the format. It returns ErrNoFormat when the radio has no owner or sent
// no such text, and the errors of FormatOf.
func RadioFormat(db *gorm.DB, radio uint) (*Format, error) {
	_, f, err := RadioText(db, radio)
	return f, err
}

// RadioText returns the text that RadioFormat reads the format of the radio
// from, with that format.
func RadioText(db *gorm.DB, radio uint) (*models.Message, *Format, error) {
	owner, ok, err := models.RadioIDOwner(db, radio)
	if err != nil {
		return nil, nil, fmt.Errorf("storeforward: finding the owner of radio %d: %w", radio, err)
	}
	if !ok {
		return nil, nil, ErrNoFormat
	}
	var m models.Message
	err = db.Where("source_id = ? AND is_group = ? AND format IN ?", radio, false, TextFormats()).
		Where("repeater_id IN (SELECT id FROM repeaters WHERE owner_id = ? OR trusted = ?)", owner, true).
		Order("received_at DESC").Order("id DESC").Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrNoFormat
	}
	if err != nil {
		return nil, nil, fmt.Errorf("storeforward: finding the last text of radio %d: %w", radio, err)
	}
	f, err := FormatOf(&m)
	if err != nil {
		return nil, nil, err
	}
	return &m, f, nil
}

// MessageText returns the text id from radio to dst, with its format,
// when it came through a repeater of the radio's owner or one that an
// admin trusts, as RadioText does for the last text of a radio. It returns
// ErrNoFormat for any other message, and the errors of FormatOf.
func MessageText(db *gorm.DB, id uint64, radio, dst uint) (*models.Message, *Format, error) {
	owner, ok, err := models.RadioIDOwner(db, radio)
	if err != nil {
		return nil, nil, fmt.Errorf("storeforward: finding the owner of radio %d: %w", radio, err)
	}
	if !ok {
		return nil, nil, ErrNoFormat
	}
	var found []models.Message
	err = db.Where("id = ? AND source_id = ? AND destination_id = ? AND is_group = ?", id, radio, dst, false).
		Where("repeater_id IN (SELECT id FROM repeaters WHERE owner_id = ? OR trusted = ?)", owner, true).
		Limit(1).Find(&found).Error
	if err != nil {
		return nil, nil, fmt.Errorf("storeforward: finding text %d of radio %d: %w", id, radio, err)
	}
	if len(found) == 0 {
		return nil, nil, ErrNoFormat
	}
	f, err := FormatOf(&found[0])
	if err != nil {
		return nil, nil, err
	}
	return &found[0], f, nil
}

// Heard sends the texts held for radio, which the hub heard on the
// repeater and slot, once the radio is quiet. One delivery per radio runs
// on a replica at a time.
func (s *Service) Heard(radio, repeater uint, slot bool) {
	var n int64
	if err := s.db.Model(&models.HeldMessage{}).Where("radio_id = ? AND state = ?", radio, models.HeldStateHeld).Count(&n).Error; err != nil {
		slog.Error("Store and forward: cannot count the held texts of a radio", "radio", radio, "error", err)
		return
	}
	if n == 0 {
		return
	}
	s.mu.Lock()
	if s.sending[radio] {
		s.mu.Unlock()
		return
	}
	s.sending[radio] = true
	s.mu.Unlock()
	s.wg.Add(1)
	s.deliveries.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.deliveries.Done()
		defer func() {
			s.mu.Lock()
			delete(s.sending, radio)
			s.mu.Unlock()
		}()
		s.deliver(radio, repeater, slot)
	}()
}

func (s *Service) waitQuiet(sender Sender, radio uint) bool {
	for {
		last, ok := sender.LastBurst(radio)
		wait := IdleGap - s.clock.Since(last)
		if !ok || wait <= 0 {
			return true
		}
		select {
		case <-s.quit:
			return false
		case <-s.clock.After(wait):
		}
	}
}

func (s *Service) deliver(radio, repeater uint, slot bool) {
	sender := s.getSender()
	if sender == nil || !s.waitQuiet(sender, radio) {
		return
	}
	if s.flush != nil {
		s.flush()
	}
	f, err := RadioFormat(s.db, radio)
	if err != nil {
		slog.Info("Store and forward: the text format of the radio is not known, its texts stay held", "radio", radio, "error", err)
		return
	}
	token := s.claimToken()
	now := s.clock.Now()
	res := s.db.Model(&models.HeldMessage{}).Where("radio_id = ? AND state = ?", radio, models.HeldStateHeld).
		Updates(map[string]any{colState: models.HeldStateSending, colClaimedBy: token, "claimed_at": now.UTC()})
	if res.Error != nil {
		slog.Error("Store and forward: cannot claim the held texts of a radio", "radio", radio, "error", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		return
	}
	var rows []models.HeldMessage
	if err := s.db.Preload("Message").Where("claimed_by = ? AND state = ?", token, models.HeldStateSending).
		Order("held_at").Order("id").Find(&rows).Error; err != nil {
		slog.Error("Store and forward: cannot load the claimed texts of a radio", "radio", radio, "error", err)
		return
	}
	stop := false
	for i := range rows {
		if stop {
			s.release(&rows[i], false)
			continue
		}
		stop = !s.send(sender, f, &rows[i], repeater, slot)
	}
}

func (s *Service) claimToken() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return s.replica + ":" + hex.EncodeToString(b[:])
}

func (s *Service) finish(row *models.HeldMessage, state models.HeldState, resp models.CallResponse, attempt bool) {
	now := s.clock.Now().UTC()
	updates := map[string]any{colState: state, "response": resp, colDoneAt: now, colClaimedBy: ""}
	if attempt {
		updates[colAttempts] = row.Attempts + 1
	}
	if err := s.db.Model(&models.HeldMessage{}).Where("id = ? AND claimed_by = ?", row.ID, row.ClaimedBy).Updates(updates).Error; err != nil {
		slog.Error("Store and forward: cannot save a held text", "id", row.ID, "error", err)
	}
}

func (s *Service) release(row *models.HeldMessage, attempt bool) {
	updates := map[string]any{colState: models.HeldStateHeld, colClaimedBy: ""}
	if attempt {
		updates[colAttempts] = row.Attempts + 1
	}
	if err := s.db.Model(&models.HeldMessage{}).Where("id = ? AND claimed_by = ?", row.ID, row.ClaimedBy).Updates(updates).Error; err != nil {
		slog.Error("Store and forward: cannot give back a held text", "id", row.ID, "error", err)
	}
}

func (s *Service) send(sender Sender, f *Format, row *models.HeldMessage, repeater uint, slot bool) bool {
	m := row.Message
	if m == nil || m.Text == nil {
		s.finish(row, models.HeldStateFailed, models.CallResponseNone, false)
		return true
	}
	if s.clock.Since(row.HeldAt) > s.limits().HoldFor() {
		s.finish(row, models.HeldStateExpired, models.CallResponseNone, false)
		return true
	}
	msg, err := f.Build(*m.Text, m.SourceID, row.RadioID)
	if err != nil {
		slog.Info("Store and forward: the text does not fit the format of the radio", "id", row.ID, "radio", row.RadioID, "error", err)
		s.finish(row, models.HeldStateFailed, models.CallResponseNone, false)
		return true
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.quit:
			cancel()
		case <-ctx.Done():
		}
	}()
	result, err := sender.SendData(ctx, repeater, slot, msg)
	if err != nil {
		slog.Error("Store and forward: cannot send a held text", "id", row.ID, "radio", row.RadioID, "error", err)
		s.release(row, false)
		return false
	}
	switch result {
	case data.ResultSent:
		s.finish(row, models.HeldStateDelivered, models.CallResponseNone, true)
	case data.ResultDelivered:
		s.finish(row, models.HeldStateDelivered, models.CallResponseACK, true)
	case data.ResultRefused:
		s.finish(row, models.HeldStateFailed, models.CallResponseNACK, true)
	case data.ResultCancelled:
		s.release(row, false)
		return false
	case data.ResultRetryLimit, data.ResultTimeLimit:
		if row.Attempts+1 >= maxAttempts {
			s.finish(row, models.HeldStateFailed, models.CallResponseNone, true)
		} else {
			s.release(row, true)
		}
		return false
	default:
		s.release(row, true)
		return false
	}
	slog.Info("Store and forward: sent a held text", "id", row.ID, "radio", row.RadioID, "src", m.SourceID, "result", result.String())
	return true
}
