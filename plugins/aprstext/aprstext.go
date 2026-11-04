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

// Package aprstext is a DMRHub plugin for two-way APRS text messages. A
// user who turned its switch on in Settings sends a private text message
// to the plugin's ID as "CALL-SSID message", and the plugin sends it on
// the hub's APRS-IS login from the user's APRS address, with a message
// number, again until the addressee acks it. An APRS message to the
// user's address comes to the user's radio as a text from the plugin's ID,
// as "CALL-SSID: message", and the plugin acks it once the hub sent it.
package aprstext

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

const (
	// MaxText is the longest text of one APRS message. A longer text goes
	// out as several messages.
	MaxText = 67
	// maxAddressee is the longest APRS addressee.
	maxAddressee = 9
	// FirstRetry is how long the plugin waits for an ack before it sends a
	// message again. Each later wait is twice the one before.
	FirstRetry = 30 * time.Second
	// Sends is how many times the plugin sends a message that gets no ack.
	Sends = 5
	// SeenFor is how long the plugin keeps the number of a message that it
	// got, so that it sends a retry of it to the radio only once.
	SeenFor = time.Hour
	// maxNumber is the highest message number before the numbers start
	// again at 1.
	maxNumber = 99999
	// maxHeard bounds the radios whose last time heard the plugin keeps.
	maxHeard = 4096
	// heardFor is how long the plugin keeps when it heard a radio.
	heardFor = 24 * time.Hour
	// requestWait bounds one request to the hub.
	requestWait = time.Minute
	// numberKey is the store key of the last message number.
	numberKey = "number"
	// seenPrefix starts the store keys of the messages that the plugin got.
	seenPrefix = "seen/"
)

// Usage is the answer to a text that is not "CALL-SSID message".
const Usage = "To send an APRS message, type the APRS address, a space, then your message, such as: N0CALL-9 See you at six"

// Hub is what the plugin asks of its hub. *plugin.Conn is a Hub.
type Hub interface {
	Has(f pluginv1.Feature) bool
	Subscribe(sub *pluginv1.Subscribe) error
	LookupRadio(ctx context.Context, radio uint32) (*pluginv1.User, error)
	LookupCallsign(ctx context.Context, callsign string) (*pluginv1.User, error)
	SendText(ctx context.Context, source, radio uint32, body string) (string, error)
	ReplyText(ctx context.Context, source uint32, text *pluginv1.Text, body string) (string, error)
	SendAPRS(ctx context.Context, m *pluginv1.APRSSend) error
	StoreGet(ctx context.Context, key string) ([]byte, error)
	StorePut(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type pendingKey struct {
	from, to, number string
}

// outgoing is an APRS message that waits for its ack.
type outgoing struct {
	msg   *pluginv1.APRSSend
	sends int
	timer *time.Timer
}

// Service is the plugin on one connection to its hub. Its methods are
// safe for concurrent use.
type Service struct {
	hub Hub
	ctx context.Context //nolint:containedctx // the retries run on timers, after the call that started them

	mu        sync.Mutex
	ranges    []*pluginv1.IDRange
	addresses map[string]uint32
	heard     map[uint32]time.Time
	pending   map[pendingKey]*outgoing
	inflight  map[string]bool
	stopped   bool
	wg        sync.WaitGroup

	numberMu sync.Mutex
	number   int
	loaded   bool
}

// New returns the plugin on hub, with the IDs ranges, which serves until
// ctx ends or Stop.
func New(ctx context.Context, hub Hub, ranges []*pluginv1.IDRange) *Service {
	return &Service{
		hub: hub, ctx: ctx, ranges: ranges,
		addresses: map[string]uint32{}, heard: map[uint32]time.Time{},
		pending: map[pendingKey]*outgoing{}, inflight: map[string]bool{},
	}
}

// Serve runs the plugin on conn until the connection ends.
func Serve(ctx context.Context, conn *plugin.Conn) error {
	s := New(ctx, conn, conn.Ranges())
	defer s.Stop()
	if err := s.Start(); err != nil {
		return err
	}
	for {
		m, err := conn.Recv()
		if err != nil {
			return err //nolint:wrapcheck // the SDK's error
		}
		s.Handle(m) //nolint:contextcheck // the service works under the context of its connection
	}
}

// Start subscribes to the texts to the plugin's IDs, the users' switches
// and the radios that the hub hears.
func (s *Service) Start() error {
	return s.subscribe(nil)
}

// Stop ends the retries and waits for the work that is under way.
func (s *Service) Stop() {
	s.mu.Lock()
	s.stopped = true
	for k, o := range s.pending {
		o.timer.Stop()
		delete(s.pending, k)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// Handle acts on a message of the hub.
func (s *Service) Handle(m *pluginv1.ConnectResponse) {
	switch msg := m.GetMessage().(type) {
	case *pluginv1.ConnectResponse_Text:
		s.spawn(func() { s.Text(msg.Text) })
	case *pluginv1.ConnectResponse_Aprs:
		s.spawn(func() { s.APRS(msg.Aprs) })
	case *pluginv1.ConnectResponse_UserSwitches:
		s.setSwitches(msg.UserSwitches.GetSwitches(), true)
	case *pluginv1.ConnectResponse_UserSwitch:
		s.setSwitches([]*pluginv1.UserSwitch{msg.UserSwitch}, false)
	case *pluginv1.ConnectResponse_Heard:
		s.noteHeard(msg.Heard)
	case *pluginv1.ConnectResponse_Ranges:
		s.mu.Lock()
		s.ranges = msg.Ranges.GetRanges()
		s.mu.Unlock()
	}
}

func (s *Service) spawn(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.wg.Go(f)
}

// subscribe asks the hub for what the plugin serves, with the APRS
// messages to addresses.
func (s *Service) subscribe(addresses []string) error {
	sub := &pluginv1.Subscribe{
		Texts:    s.hub.Has(pluginv1.Feature_FEATURE_TEXTS),
		Switches: s.hub.Has(pluginv1.Feature_FEATURE_SWITCHES),
		Heard:    s.hub.Has(pluginv1.Feature_FEATURE_HEARD),
	}
	if s.hub.Has(pluginv1.Feature_FEATURE_APRS_INBOX) {
		sub.Aprs = addresses
	}
	if err := s.hub.Subscribe(sub); err != nil {
		return err //nolint:wrapcheck // the SDK's error
	}
	return nil
}

// Address returns the APRS address of a user's switch.
func Address(callsign, choice string) string {
	if choice == "" {
		choice = PresetSSID
	}
	return strings.ToUpper(callsign) + "-" + choice
}

// setSwitches notes the APRS addresses of the switches in list, all of
// the users who turned the switch on when all, and subscribes to them.
func (s *Service) setSwitches(list []*pluginv1.UserSwitch, all bool) {
	s.mu.Lock()
	before := slices.Sorted(maps.Keys(s.addresses))
	if all {
		s.addresses = map[string]uint32{}
	}
	for _, sw := range list {
		if sw.GetKey() != SwitchKey {
			continue
		}
		for a, id := range s.addresses {
			if id == sw.GetUserId() {
				delete(s.addresses, a)
			}
		}
		if sw.GetOn() {
			s.addresses[Address(sw.GetCallsign(), sw.GetChoice())] = sw.GetUserId()
		}
	}
	after := slices.Sorted(maps.Keys(s.addresses))
	s.mu.Unlock()
	if slices.Equal(before, after) {
		return
	}
	if err := s.subscribe(after); err != nil {
		slog.Error("APRS messages: cannot subscribe to the APRS addresses", "error", err)
	}
}

func (s *Service) noteHeard(h *pluginv1.Heard) {
	at := time.Now()
	if h.At != nil {
		at = time.UnixMilli(h.GetAt())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteRadio(h.GetRadio(), at)
}

func (s *Service) noteRadio(radio uint32, at time.Time) {
	if len(s.heard) >= maxHeard {
		for r, t := range s.heard {
			if time.Since(t) > heardFor {
				delete(s.heard, r)
			}
		}
		if len(s.heard) >= maxHeard {
			s.heard = map[uint32]time.Time{}
		}
	}
	if at.After(s.heard[radio]) {
		s.heard[radio] = at
	}
}

// id returns the first of the plugin's IDs, or 0 when it has none.
func (s *Service) id() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ranges) == 0 {
		return 0
	}
	return s.ranges[0].GetFirst()
}

func (s *Service) request() (context.Context, context.CancelFunc) {
	return context.WithTimeout(s.ctx, requestWait)
}

// switchOn returns the APRS address of u when u turned the switch on.
func switchOn(u *pluginv1.User) (string, bool) {
	for _, sw := range u.GetSwitches() {
		if sw.GetKey() == SwitchKey && sw.GetOn() {
			return Address(u.GetCallsign(), sw.GetChoice()), true
		}
	}
	return "", false
}

// Text sends the text message t, from a radio of a user who turned the
// switch on, to APRS-IS. It does nothing for a radio of no such user.
func (s *Service) Text(t *pluginv1.Text) {
	if t.GetGroup() {
		return
	}
	ctx, cancel := s.request()
	defer cancel()
	u, err := s.hub.LookupRadio(ctx, t.GetSource())
	if err != nil {
		if !errors.Is(err, plugin.ErrNotFound) {
			slog.Error("APRS messages: cannot look up a radio", "radio", t.GetSource(), "error", err)
		}
		return
	}
	from, on := switchOn(u)
	if !on {
		return
	}
	s.mu.Lock()
	s.noteRadio(t.GetSource(), time.UnixMilli(t.GetAt()))
	s.mu.Unlock()
	to, text, ok := ParseText(t.GetBody())
	if !ok {
		s.answer(t, Usage)
		return
	}
	for _, part := range Split(Clean(text), MaxText) {
		n, err := s.nextNumber()
		if err != nil {
			slog.Error("APRS messages: cannot number a message", "error", err)
			return
		}
		m := &pluginv1.APRSSend{Source: from, Addressee: to, Text: part, Number: n}
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return
		}
		o := &outgoing{msg: m}
		key := pendingKey{from: from, to: to, number: n}
		s.pending[key] = o
		s.mu.Unlock()
		s.retry(key, o)
	}
}

// answer sends body to the radio of t, in the exact format of t. It sends
// nothing when the hub cannot build that format again.
func (s *Service) answer(t *pluginv1.Text, body string) {
	ctx, cancel := s.request()
	defer cancel()
	if _, err := s.hub.ReplyText(ctx, t.GetDestination(), t, body); err != nil {
		slog.Warn("APRS messages: cannot answer a radio", "radio", t.GetSource(), "error", err)
	}
}

// retry sends the message of o, and sets the timer of its next send, or of
// its end after the last one.
func (s *Service) retry(key pendingKey, o *outgoing) {
	ctx, cancel := s.request()
	err := s.hub.SendAPRS(ctx, o.msg)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending[key] != o {
		return
	}
	if errors.Is(err, plugin.ErrRefused) || errors.Is(err, plugin.ErrInvalid) {
		slog.Warn("APRS messages: the hub refused a message", "source", o.msg.GetSource(), "error", err)
		delete(s.pending, key)
		return
	}
	if err != nil {
		slog.Warn("APRS messages: cannot send a message", "source", o.msg.GetSource(), "error", err)
	}
	wait := FirstRetry << o.sends
	o.sends++
	if o.sends >= Sends {
		o.timer = time.AfterFunc(wait, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.pending[key] == o {
				delete(s.pending, key)
			}
		})
		return
	}
	o.timer = time.AfterFunc(wait, func() { s.spawn(func() { s.retry(key, o) }) })
}

// acked ends the retries of the message number that the address from sent
// to the address to.
func (s *Service) acked(from, to, number string) {
	number, _, _ = strings.Cut(number, "}")
	key := pendingKey{from: from, to: strings.ToUpper(to), number: number}
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.pending[key]; ok {
		if o.timer != nil {
			o.timer.Stop()
		}
		delete(s.pending, key)
	}
}

// Pending returns how many messages wait for their acks.
func (s *Service) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

func (s *Service) nextNumber() (string, error) {
	s.numberMu.Lock()
	defer s.numberMu.Unlock()
	ctx, cancel := s.request()
	defer cancel()
	if !s.loaded {
		b, err := s.hub.StoreGet(ctx, numberKey)
		switch {
		case errors.Is(err, plugin.ErrNotFound):
		case err != nil:
			return "", err //nolint:wrapcheck // the SDK's error
		default:
			s.number, _ = strconv.Atoi(string(b))
		}
		s.loaded = true
	}
	s.number = s.number%maxNumber + 1
	n := strconv.Itoa(s.number)
	if err := s.hub.StorePut(ctx, numberKey, []byte(n), 0); err != nil {
		return "", err //nolint:wrapcheck // the SDK's error
	}
	return n, nil
}

// isAck returns the number of an ack or reject in text.
func isAck(text string) (string, bool) {
	if len(text) > 3 {
		switch strings.ToLower(text[:3]) {
		case "ack", "rej":
			return strings.TrimSpace(text[3:]), true
		}
	}
	return "", false
}

// APRS sends m, an APRS message to the address of a user who turned the
// switch on, to that user's radio, and acks it once the hub sent it. It
// ends the retries of a message that m acks.
func (s *Service) APRS(m *pluginv1.APRSMessage) {
	to := strings.ToUpper(strings.TrimSpace(m.GetAddressee()))
	from := strings.ToUpper(strings.TrimSpace(m.GetSource()))
	s.mu.Lock()
	_, ours := s.addresses[to]
	s.mu.Unlock()
	if !ours || from == "" {
		return
	}
	number := m.GetNumber()
	if n, ok := isAck(m.GetText()); ok && number == "" {
		s.acked(to, from, n)
		return
	}
	if _, ack, reply := strings.Cut(number, "}"); reply && ack != "" {
		s.acked(to, from, ack)
	}
	seen := seenPrefix + to + "/" + from + "/" + number
	if number != "" {
		s.mu.Lock()
		busy := s.inflight[seen]
		if !busy {
			s.inflight[seen] = true
		}
		s.mu.Unlock()
		if busy {
			return
		}
		defer func() {
			s.mu.Lock()
			delete(s.inflight, seen)
			s.mu.Unlock()
		}()
		ctx, cancel := s.request()
		_, err := s.hub.StoreGet(ctx, seen)
		cancel()
		if err == nil {
			s.ack(to, from, number)
			return
		}
	}
	if !s.deliver(to, from+": "+strings.TrimSpace(m.GetText())) {
		return
	}
	if number != "" {
		ctx, cancel := s.request()
		if err := s.hub.StorePut(ctx, seen, []byte{1}, SeenFor); err != nil {
			slog.Warn("APRS messages: cannot note a message", "error", err)
		}
		cancel()
		s.ack(to, from, number)
	}
}

func (s *Service) ack(from, to, number string) {
	ctx, cancel := s.request()
	defer cancel()
	if err := s.hub.SendAPRS(ctx, &pluginv1.APRSSend{Source: from, Addressee: to, Text: "ack" + number}); err != nil {
		slog.Warn("APRS messages: cannot ack a message", "source", from, "addressee", to, "error", err)
	}
}

// deliver sends body to a radio of the user of the address to, the one the
// hub heard last first, and reports whether the hub sent it. It tries the
// next radio only while the hub knows no text format of a radio, since it
// then sent nothing.
func (s *Service) deliver(to, body string) bool {
	source := s.id()
	if source == 0 {
		return false
	}
	ctx, cancel := s.request()
	defer cancel()
	base, _, _ := strings.Cut(to, "-")
	u, err := s.hub.LookupCallsign(ctx, base)
	if err != nil {
		if !errors.Is(err, plugin.ErrNotFound) {
			slog.Error("APRS messages: cannot look up a callsign", "callsign", base, "error", err)
		}
		return false
	}
	if address, on := switchOn(u); !on || address != to {
		return false
	}
	radios := slices.Clone(u.GetRadios())
	s.mu.Lock()
	slices.SortStableFunc(radios, func(a, b uint32) int { return s.heard[b].Compare(s.heard[a]) })
	s.mu.Unlock()
	for _, r := range radios {
		_, err := s.hub.SendText(ctx, source, r, body)
		switch {
		case err == nil:
			return true
		case errors.Is(err, plugin.ErrNoFormat):
			continue
		default:
			slog.Warn("APRS messages: cannot send a message to a radio", "radio", r, "error", err)
			return false
		}
	}
	return false
}

// ParseText reads "CALL-SSID message" from body: the addressee, in upper
// case, and the text. It returns false when body is not that.
func ParseText(body string) (string, string, bool) {
	to, text, _ := strings.Cut(strings.TrimSpace(body), " ")
	to = strings.ToUpper(to)
	text = strings.TrimSpace(text)
	if to == "" || len(to) > maxAddressee || text == "" {
		return "", "", false
	}
	for _, r := range to {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return "", "", false
		}
	}
	return to, text, true
}

// Clean returns text with what APRS cannot carry replaced: "|", "~" and
// "{", which APRS keeps for itself, line breaks and tabs with spaces, and
// what is not printable ASCII with "?".
func Clean(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '|':
			b.WriteByte('/')
		case r == '~':
			b.WriteByte('-')
		case r == '{':
			b.WriteByte('(')
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r > 0x7e:
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Split splits text into parts of at most limit bytes, between words where
// it can.
func Split(text string, limit int) []string {
	var parts []string
	for len(text) > limit {
		cut := strings.LastIndexByte(text[:limit+1], ' ')
		if cut <= 0 {
			cut = limit
		}
		parts = append(parts, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}
