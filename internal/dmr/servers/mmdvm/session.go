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

package mmdvm

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm/clause"
)

// beaconDelay is the time after a login at which the server asks the
// repeater for a beacon.
const beaconDelay = time.Minute

// egressResumeRetry is how long resumeForEgress waits before it looks again
// for the KV record of a repeater that it could not resume.
const egressResumeRetry = time.Second

// maxPending is the most sessions that are not logged in. LoginTimeout ends
// each of them, but RPTLs from many spoofed addresses could still fill the
// session table faster than that.
const maxPending = 1024

var errTooManyPending = errors.New("too many sessions that are not logged in")

var errRecordsBusy = errors.New("too many record jobs wait for the repeater")

// link is the session of the repeater at one address.
type link struct {
	key     string
	ep      endpoint
	dialect dmrd.Dialect
	m       *dmrdsession.Master
	beacon  *time.Timer
	// looking reports that the password of a login of the link is being
	// looked up, or its config saved.
	looking bool
	// moved reports that the session was taken over from a draining
	// replica at a new address, with no login since. Its options are not
	// taken, as nothing but the address vouches for it.
	moved bool
	// heard is when the server read the packet of the link that it
	// handles.
	heard time.Time
}

func (s *Server) now() time.Duration {
	if s.epoch.IsZero() {
		s.epoch = time.Now()
	}
	return time.Since(s.epoch)
}

// packetDialect returns the dialect of the packet p from a repeater: the
// MMDVM network protocol or HBRP. ok is false for a packet that is valid in
// both, which only DMRD is, and for one that is valid in neither. Every
// other message that the two share has a different length in each: an ID
// is 4 binary octets in the MMDVM network protocol and 8 hex digits in
// HBRP, and the RPTC is 302 octets in one and 306 in the other.
func packetDialect(p []byte) (d dmrd.Dialect, ok bool) {
	_, mmdvm := dmrd.RepeaterID(p, dmrd.DialectMMDVM)
	_, hbrp := dmrd.RepeaterID(p, dmrd.DialectHBRP)
	switch {
	case mmdvm && !hbrp:
		return dmrd.DialectMMDVM, true
	case hbrp && !mmdvm:
		return dmrd.DialectHBRP, true
	}
	return dmrd.DialectUnknown, false
}

// loginDialect returns the dialect of p when p is an RPTL.
func loginDialect(p []byte) (dmrd.Dialect, bool) {
	d, ok := packetDialect(p)
	return d, ok && dmrd.Classify(p, d) == dmrd.CommandRPTL
}

// storedDialect returns the dialect that the KV record of a repeater names.
// A record without one is from a DMRHub that spoke only the MMDVM network
// protocol.
func storedDialect(repeater models.Repeater) dmrd.Dialect {
	if repeater.Dialect == dmrd.DialectHBRP.String() {
		return dmrd.DialectHBRP
	}
	return dmrd.DialectMMDVM
}

// kvRecord is the KV record of a repeater, as looked up before the packet
// that needs it is handled. moved reports that the packet may resume the
// session from a new address, as moved decides.
type kvRecord struct {
	repeater models.Repeater
	moved    bool
	// current is set when the record came from a login with the password
	// that the repeater has now.
	current bool
	// rekeys is the count of password changes of the repeater before the
	// record was read.
	rekeys uint64
}

// recordFunc returns the KV record of the repeater id, as looked up before
// the packet that needs it is handled.
type recordFunc func(id uint32) (kvRecord, bool)

func noRecord(uint32) (kvRecord, bool) { return kvRecord{}, false }

// maxHeldPerAddress is the most packets from one address that wait for the
// KV record of their repeater.
const maxHeldPerAddress = 64

// dialectFor returns the dialect of the session that the packet p from
// addr starts: the dialect of p, or for a DMRD the dialect of the session
// that the KV store records for its repeater at addr, or else the MMDVM
// network protocol.
func dialectFor(ep endpoint, p []byte, record recordFunc) dmrd.Dialect {
	if d, ok := packetDialect(p); ok {
		return d
	}
	if id, ok := dmrd.RepeaterID(p, dmrd.DialectMMDVM); ok {
		if rec, ok := record(id); ok && (rec.moved || ep.matchesKV(rec.repeater.IP, rec.repeater.Port)) {
			return storedDialect(rec.repeater)
		}
	}
	return dmrd.DialectMMDVM
}

// resumable reports whether a packet data in dialect d may resume the
// session that another replica had.
func resumable(data []byte, d dmrd.Dialect) bool {
	switch dmrd.Classify(data, d) { //nolint:exhaustive
	case dmrd.CommandDMRD, dmrd.CommandDMRA, dmrd.CommandDMRG, dmrd.CommandRPTPING, dmrd.CommandMSTPING, dmrd.CommandRPTO:
		return true
	default:
		return false
	}
}

// recordNeeded returns the ID of the repeater whose KV record the packet
// data needs, to pick the dialect of a new session or to resume one, when
// l, the session at its address or nil, is idle.
func (s *Server) recordNeeded(l *link, data []byte) (uint32, bool) {
	if s.kvClient == nil || (l != nil && l.m.State() != dmrdsession.StateIdle) {
		return 0, false
	}
	d, known := packetDialect(data)
	if l != nil {
		d, known = l.dialect, true
	}
	if !known {
		return dmrd.RepeaterID(data, dmrd.DialectMMDVM)
	}
	if !resumable(data, d) {
		return 0, false
	}
	return dmrd.RepeaterID(data, d)
}

// resolve looks up the KV record of the repeater id off the listener, then
// handles the packets from remote that waited for it, in order. It reads
// the record after the record jobs of the repeater that are queued, so
// that a session that ended here never resumes from the record that its
// end deletes.
func (s *Server) resolve(ctx context.Context, remote endpoint, key string, id uint32, data []byte, rekeys uint64) {
	defer s.lookups.Done()
	var repeater models.Repeater
	err := errRecordsBusy
	s.records.After(uint(id), func() { repeater, err = s.kvClient.GetRepeater(ctx, uint(id)) })
	if kvDown(err) && !errors.Is(err, errRecordsBusy) {
		if own, ok := s.ownRecord(uint(id)); ok && remote.matchesKV(own.IP, own.Port) {
			repeater, err = own, nil
		}
	}
	rec := kvRecord{repeater: repeater, rekeys: rekeys}
	if err == nil && repeater.Connection == models.RepeaterStateConnected {
		rec.current = s.loggedInWithCurrentSecret(uint(id), repeater)
		rec.moved = rec.current && s.moved(ctx, remote, data, repeater, id)
	}
	if rec.moved {
		s.awaitHandover(ctx, uint(id))
	}
	record := func(x uint32) (kvRecord, bool) { return rec, err == nil && x == id }
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.resolving[key]
	delete(s.resolving, key)
	if s.stopped.Load() {
		return
	}
	for _, p := range held {
		s.handleLocked(ctx, remote, p.data, p.at, record)
	}
}

func newMaster(d dmrd.Dialect) (*dmrdsession.Master, error) {
	m, err := dmrdsession.NewMaster(&dmrdsession.MasterConfig{
		Dialect:      d,
		LoginTimeout: dmrdsession.LoginTimeoutDefault,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to make %s session: %w", d, err)
	}
	return m, nil
}

// linkFor returns the session at the UDP address addr, or a new one in
// dialect d.
func (s *Server) linkFor(addr net.UDPAddr, d dmrd.Dialect) (*link, error) {
	return s.linkAt(udpEndpoint(addr), d)
}

// linkAt returns the session at ep, or a new one in dialect d.
func (s *Server) linkAt(ep endpoint, d dmrd.Dialect) (*link, error) {
	if s.links == nil {
		s.links = map[string]*link{}
		s.byID = map[uint32]*link{}
	}
	key := ep.String()
	if l, ok := s.links[key]; ok {
		return l, nil
	}
	if len(s.links)-len(s.byID) >= maxPending {
		return nil, errTooManyPending
	}
	m, err := newMaster(d)
	if err != nil {
		return nil, err
	}
	l := &link{key: key, ep: ep, dialect: d, m: m}
	s.links[key] = l
	return l, nil
}

// redial ends the session of l, as a new RPTL ends it, and gives l a new
// session in dialect d, for a repeater that logs in again in the other
// dialect from the same address.
func (s *Server) redial(ctx context.Context, l *link, d dmrd.Dialect) error {
	m, err := newMaster(d)
	if err != nil {
		return err
	}
	if st := l.m.State(); st != dmrdsession.StateIdle {
		id := l.m.ID()
		l.m.Reset()
		s.down(ctx, l, dmrdsession.Output{Kind: dmrdsession.OutputDown, ID: id, Reason: dmrdsession.ReasonLogin, State: st})
	}
	l.m, l.dialect = m, d
	return nil
}

func (s *Server) forget(l *link) {
	if l.m.State() == dmrdsession.StateIdle && s.links[l.key] == l {
		delete(s.links, l.key)
	}
}

// handlePacket handles the packet data from remoteAddr as the listener
// does, and waits for the lookups and the repeater jobs that it started.
func (s *Server) handlePacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	s.handleFrom(ctx, udpEndpoint(remoteAddr), data, time.Now())
	s.lookups.Wait()
	s.records.Wait()
}

func (s *Server) handleFrom(ctx context.Context, remote endpoint, data []byte, at time.Time) {
	if s.stopped.Load() {
		return
	}

	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Load() {
		return
	}
	key := remote.String()
	if held, ok := s.resolving[key]; ok {
		if len(held) < maxHeldPerAddress {
			s.resolving[key] = append(held, heldPacket{data: data, at: at})
		}
		return
	}
	if id, ok := s.recordNeeded(s.links[key], data); ok {
		if len(s.resolving) >= maxPending {
			return
		}
		if s.resolving == nil {
			s.resolving = map[string][]heldPacket{}
		}
		s.resolving[key] = []heldPacket{{data: data, at: at}}
		s.lookups.Add(1)
		go s.resolve(ctx, remote, key, id, data, s.rekeys[id])
		return
	}
	s.handleLocked(ctx, remote, data, at, noRecord)
}

// heldPacket is a packet that waits for a KV record, and at is when the
// server read it.
type heldPacket struct {
	data []byte
	at   time.Time
}

// handleLocked handles the packet data from remote, which the server read
// at at, with s.mu held. record returns the KV records that recordNeeded
// asked for.
func (s *Server) handleLocked(ctx context.Context, remote endpoint, data []byte, at time.Time, record recordFunc) {
	l, ok := s.links[remote.String()]
	if !ok {
		var err error
		if l, err = s.linkAt(remote, dialectFor(remote, data, record)); err != nil {
			slog.Error("Error making session", "remoteAddr", remote.String(), "error", err)
			return
		}
	} else if d, ok := loginDialect(data); ok && d != l.dialect {
		if err := s.redial(ctx, l, d); err != nil {
			slog.Error("Error making session", "remoteAddr", remote.String(), "error", err)
			return
		}
	}
	now := s.now()
	l.heard = at
	if l.m.State() == dmrdsession.StateIdle {
		s.resume(ctx, now, l, data, record)
	}
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	s.run(ctx, now, l, l.m.Packet(now, data, buf[:0]))
	s.forget(l)
}

func (s *Server) tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, l := range s.links {
		var buf [dmrdsession.MaxOutputs]dmrdsession.Output
		s.run(ctx, now, l, l.m.Tick(now, buf[:0]))
		s.forget(l)
	}
}

func (s *Server) closeSessions(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slog.Info("Stopping MMDVM server, sending MSTCL to connected repeaters", "count", len(s.byID))
	now := s.now()
	for _, l := range s.links {
		var buf [dmrdsession.MaxOutputs]dmrdsession.Output
		s.run(ctx, now, l, l.m.Close(now, buf[:0]))
	}
	clear(s.links)
}

func (s *Server) write(ep endpoint, p []byte) {
	t, ok := s.transport(ep.transport)
	if !ok {
		slog.Error("Error sending packet: no such transport", "remoteAddr", ep.String())
		return
	}
	if _, err := t.WriteTo(p, ep.addr); err != nil {
		slog.Error("Error sending packet", "remoteAddr", ep.String(), "error", err)
	}
}

// run carries out the outputs of a session. DMRHub accepts a config that it
// can read and refuses the login for any other.
func (s *Server) run(ctx context.Context, now time.Duration, l *link, out []dmrdsession.Output) {
	for _, o := range out {
		switch o.Kind {
		case dmrdsession.OutputSend:
			s.write(l.ep, o.Packet)
		case dmrdsession.OutputLogin:
			s.login(ctx, now, l, o.ID)
		case dmrdsession.OutputUp:
			s.up(ctx, l, o.ID)
		case dmrdsession.OutputDown:
			s.down(ctx, l, o)
		case dmrdsession.OutputOptions:
			s.options(ctx, l, o)
		case dmrdsession.OutputData:
			s.data(ctx, o, l.heard)
		case dmrdsession.OutputMessage:
			slog.Debug("Repeater message", "repeaterID", o.ID, "command", o.Command)
		case dmrdsession.OutputPong:
			s.ping(ctx, l)
		case dmrdsession.OutputConfig:
			s.configLater(ctx, l, o.Packet)
		}
	}
}

// salt returns a new random login salt.
func (s *Server) salt() (uint32, error) {
	if s.newSalt != nil {
		return s.newSalt()
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("failed to read random salt: %w", err)
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

// login looks up the password of the login of id on l off the listener, so
// that a flood of logins never holds up the packets of other repeaters or a
// real login. Each link looks up one login at a time. Once the IDs of the
// repeaters are loaded, a login of an ID that is not among them is looked
// up only maxMisses times a second and refused at once otherwise, so that
// a flood of made-up IDs never queues ahead of a real login on the
// database.
func (s *Server) login(ctx context.Context, now time.Duration, l *link, id uint32) {
	if l.looking {
		return
	}
	if s.known != nil && !s.known[id] && !s.allowMiss(now) {
		var buf [dmrdsession.MaxOutputs]dmrdsession.Output
		s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
		return
	}
	l.looking = true
	s.lookups.Add(1)
	go s.lookup(ctx, l, id)
}

func (s *Server) lookup(ctx context.Context, l *link, id uint32) {
	defer s.lookups.Done()
	password, err := s.password(uint(id))
	s.mu.Lock()
	defer s.mu.Unlock()
	l.looking = false
	if s.stopped.Load() || s.links[l.key] != l || l.m.State() != dmrdsession.StateLogin {
		return
	}
	now := s.now()
	if l.m.ID() != id {
		s.login(ctx, now, l, l.m.ID())
		return
	}
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	if err != nil {
		slog.Debug("Refusing login", "repeaterID", id, "remoteAddr", l.ep.String(), "error", err)
		s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
		s.forget(l)
		return
	}
	salt, err := s.salt()
	if err != nil {
		slog.Error("Error generating random salt", "error", err)
		s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
		s.forget(l)
		return
	}
	slog.Debug("Login from Repeater ID", "repeaterID", id)
	l.moved = false
	if s.known != nil {
		s.known[id] = true
	}
	s.run(ctx, now, l, l.m.Admit(now, salt, []byte(password), buf[:0]))
	s.forget(l)
}

// maxMisses is how many logins of IDs that are not known repeaters are
// looked up each second.
const maxMisses = 5

func (s *Server) allowMiss(now time.Duration) bool {
	if now-s.missesAt >= time.Second {
		s.missesAt, s.misses = now, 0
	}
	if s.misses >= maxMisses {
		return false
	}
	s.misses++
	return true
}

// knownRefresh is how often the IDs of the repeaters are loaded again.
const knownRefresh = 30 * time.Second

// loadKnown loads the IDs of the repeaters, which login checks before it
// looks up a password.
func (s *Server) loadKnown() error {
	var ids []uint
	if err := s.DB.Model(&models.Repeater{}).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("failed to load the repeater IDs: %w", err)
	}
	known := make(map[uint32]bool, len(ids))
	for _, id := range ids {
		known[uint32(id)] = true //nolint:gosec
	}
	s.mu.Lock()
	s.known = known
	s.mu.Unlock()
	return nil
}

func (s *Server) refreshKnown() {
	defer s.wg.Done()
	ticker := time.NewTicker(knownRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if err := s.loadKnown(); err != nil {
				slog.Error("Error loading the repeater IDs", "error", err)
			}
		}
	}
}

var (
	errNoPassword = errors.New("repeater has no password")
	errModemID    = errors.New("repeater is the modem of this server")
)

func (s *Server) password(repeaterID uint) (string, error) {
	exists, err := models.RepeaterIDExists(s.DB, repeaterID)
	if err != nil {
		return "", fmt.Errorf("failed to find repeater: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("repeater %d does not exist", repeaterID)
	}
	repeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		return "", fmt.Errorf("failed to find repeater: %w", err)
	}
	if repeater.Type == models.RepeaterTypeModem {
		return "", errModemID
	}
	if repeater.Password == "" {
		return "", errNoPassword
	}
	return repeater.Password, nil
}

// configLater takes the RPTC data of l off the listener, as login looks up a
// password, so that the database never holds up the packets of other
// repeaters, such as when every repeater logs in again at once. Each link
// takes one config at a time; the session answers a repeated RPTC once.
func (s *Server) configLater(ctx context.Context, l *link, data []byte) {
	if l.looking {
		return
	}
	l.looking = true
	id, ep, d := l.m.ID(), l.ep, l.dialect
	data = append([]byte(nil), data...)
	s.lookups.Add(1)
	go func() {
		defer s.lookups.Done()
		ok := s.takeConfig(ctx, id, ep, d, data)
		s.mu.Lock()
		defer s.mu.Unlock()
		l.looking = false
		if s.stopped.Load() || s.links[l.key] != l || l.m.ID() != id {
			return
		}
		st := l.m.State()
		if st != dmrdsession.StateConfig && st != dmrdsession.StateRunning {
			return
		}
		now := s.now()
		var buf [dmrdsession.MaxOutputs]dmrdsession.Output
		if ok {
			s.run(ctx, now, l, l.m.Accept(now, buf[:0]))
		} else {
			s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
		}
		s.forget(l)
	}()
}

// takeConfig saves the RPTC data in dialect d of the repeater id at ep and
// reports whether DMRHub accepts it.
func (s *Server) takeConfig(ctx context.Context, id uint32, ep endpoint, d dmrd.Dialect, data []byte) bool {
	repeaterID := uint(id)
	repeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return false
	}
	parse := repeater.ParseConfig
	if d == dmrd.DialectHBRP {
		parse = repeater.ParseHBRPConfig
	}
	if err := parse(data, s.Version, s.Commit); err != nil {
		slog.Error("Invalid RPTC packet", "repeaterID", repeaterID, "error", err)
		return false
	}
	now := time.Now()
	repeater.IP, repeater.Port = ep.kvAddr()
	repeater.Connected = now
	repeater.LastPing = now
	repeater.Connection = models.RepeaterStateConnected
	repeater.Dialect = d.String()
	check, err := secretCheck(repeater.Password)
	if err != nil {
		slog.Error("Error making the secret check of a session", "repeaterID", repeaterID, "error", err)
		return false
	}
	repeater.SecretCheck = check

	dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return false
	}
	dbRepeater.UpdateFrom(repeater)
	if err := s.DB.Omit(clause.Associations).Save(&dbRepeater).Error; err != nil {
		slog.Error("Error saving repeater to database", "repeaterID", repeaterID, "error", err)
		return false
	}
	s.records.Do(repeaterID, func() {
		s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
		s.keepOwnRecord(repeaterID, repeater)
	})
	slog.Info("Repeater connected", "repeaterID", repeaterID, "callsign", repeater.Callsign)
	return true
}

func (s *Server) up(ctx context.Context, l *link, id uint32) {
	if old, ok := s.byID[id]; ok && old != l {
		s.stopBeacon(old)
		old.m.Reset()
		s.forget(old)
	}
	s.byID[id] = l
	s.connected.Store(uint(id), l.ep)
	s.stopBeacon(l)
	ep, d := l.ep, l.dialect
	l.beacon = time.AfterFunc(beaconDelay, func() { s.sendBeacon(id, ep, d) })
	if s.hub != nil {
		since := l.heard
		if since.IsZero() {
			since = time.Now()
		}
		s.records.Do(uint(id), func() { s.hub.ActivateRepeaterSince(ctx, uint(id), since) })
	}
}

// sendBeacon asks the repeater id for a beacon in dialect d when it is
// still connected at ep.
func (s *Server) sendBeacon(id uint32, ep endpoint, d dmrd.Dialect) {
	if a, ok := s.connected.Load(uint(id)); !ok || a.String() != ep.String() {
		return
	}
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTSBKN, ID: id}).AppendBinary(nil, d)
	if err == nil {
		s.write(ep, p)
	}
}

func (s *Server) stopBeacon(l *link) {
	if l.beacon != nil {
		l.beacon.Stop()
		l.beacon = nil
	}
}

func (s *Server) down(ctx context.Context, l *link, o dmrdsession.Output) {
	if s.byID[o.ID] != l {
		slog.Debug("Repeater login ended", "repeaterID", o.ID, "reason", o.Reason, "state", o.State)
		return
	}
	slog.Info("Repeater session ended", "repeaterID", o.ID, "reason", o.Reason)
	s.stopBeacon(l)
	s.forgetSent(uint(o.ID))
	delete(s.byID, o.ID)
	s.connected.Delete(uint(o.ID))
	downAt := time.Now()
	s.records.Do(uint(o.ID), func() {
		owned := s.kvOwned(ctx, o, downAt)
		deleted := owned && s.kvClient.DeleteRepeater(ctx, uint(o.ID))
		if owned && !deleted {
			slog.Error("Repeater ID not deleted", "repeaterID", o.ID)
		}
		if deleted || !owned || o.Reason != dmrdsession.ReasonTimeout {
			s.dropOwnRecord(uint(o.ID))
		}
		if s.hub != nil {
			s.hub.DeactivateRepeater(ctx, uint(o.ID))
		}
	})
}

// kvOwned reports whether the KV record of the repeater of the session
// that o ended at downAt is still the record of that session. A record
// with a ping after downAt, or after a timeout a record with a ping within
// the session timeout, means that another replica took the session over
// and keeps the record.
func (s *Server) kvOwned(ctx context.Context, o dmrdsession.Output, downAt time.Time) bool {
	repeater, err := s.kvClient.GetRepeater(ctx, uint(o.ID))
	if err != nil {
		return true
	}
	if repeater.LastPing.After(downAt) {
		return false
	}
	return o.Reason != dmrdsession.ReasonTimeout || time.Since(repeater.LastPing) >= dmrdsession.TimeoutDefault
}

func (s *Server) data(ctx context.Context, o dmrdsession.Output, at time.Time) {
	switch o.Command { //nolint:exhaustive
	case dmrd.CommandDMRD:
		d, err := dmrd.DecodeData(o.Packet)
		if err != nil {
			slog.Error("Failed to decode DMRD packet", "error", err)
			return
		}
		packet := models.PacketFromData(&d, d.Size > dmrconst.MMDVMPacketLength)
		if packet.Dst == 0 {
			return
		}
		slog.Debug("DMRD packet received", "packet", packet.String())
		if s.hub != nil {
			s.hub.RoutePacketAt(ctx, packet, models.RepeaterTypeMMDVM, at)
		}
	case dmrd.CommandDMRA:
		alias, err := dmrd.DecodeTalkerAlias(o.Packet)
		if err != nil {
			slog.Error("Invalid DMRA packet", "length", len(o.Packet), "error", err)
			return
		}
		slog.Debug("Talk alias", "repeaterID", alias.Repeater, "type", alias.Type, "data", string(alias.Data[:]))
		if s.hub != nil {
			//nolint:contextcheck // the call tracker records on its own workers, which outlive the caller
			s.hub.RecordTalkerAlias(alias, models.RepeaterTypeMMDVM, uint(o.ID))
		}
	case dmrd.CommandDMRG:
		pos, err := dmrd.DecodePosition(o.Packet)
		if err != nil {
			slog.Error("Invalid DMRG packet", "length", len(o.Packet), "error", err)
			return
		}
		slog.Debug("GPS position", "repeaterID", pos.Repeater, "source", pos.Source)
		if s.hub != nil {
			//nolint:contextcheck // the call tracker records on its own workers, which outlive the caller
			s.hub.RecordPosition(pos, models.RepeaterTypeMMDVM, uint(o.ID))
		}
	default:
		slog.Debug("Repeater data", "repeaterID", o.ID, "command", o.Command)
	}
}

// ping counts a ping of the repeater of l in its KV record, after the
// other jobs of the repeater, and queues its time for the database.
func (s *Server) ping(ctx context.Context, l *link) {
	repeaterID := uint(l.m.ID())
	now := time.Now()
	ip, port := l.ep.kvAddr()
	s.records.Do(repeaterID, func() {
		repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
		if kvDown(err) {
			if own, ok := s.ownRecord(repeaterID); ok {
				own.LastPing = now
				own.IP, own.Port = ip, port
				s.keepOwnRecord(repeaterID, own)
			}
		}
		if err != nil {
			slog.Error("Error getting repeater from kv", "repeaterID", repeaterID, "error", err)
			return
		}
		repeater.PingsReceived++
		repeater.LastPing = now
		repeater.IP, repeater.Port = ip, port
		s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
		s.keepOwnRecord(repeaterID, repeater)
		if s.hub != nil {
			s.hub.RenewOwnership(ctx, repeaterID)
		}
	})
	s.notePing(repeaterID, now)
}

// notePing queues the last ping time of a repeater for the database, so
// that a slow store never holds up the packets of every repeater.
func (s *Server) notePing(repeaterID uint, at time.Time) {
	if s.DB == nil {
		return
	}
	s.pingMu.Lock()
	defer s.pingMu.Unlock()
	if s.pingsClosed {
		return
	}
	if s.pings == nil {
		s.pings = map[uint]time.Time{}
	}
	s.pings[repeaterID] = at
	if s.pinging {
		return
	}
	s.pinging = true
	s.pingWG.Add(1)
	go s.writePings()
}

func (s *Server) writePings() {
	defer s.pingWG.Done()
	for {
		s.pingMu.Lock()
		pings := s.pings
		s.pings = nil
		if len(pings) == 0 {
			s.pinging = false
			s.pingMu.Unlock()
			return
		}
		s.pingMu.Unlock()
		for id, at := range pings {
			if err := s.DB.Model(&models.Repeater{}).Where("id = ?", id).UpdateColumn("last_ping", at).Error; err != nil {
				slog.Error("Error saving the last ping of a repeater", "repeaterID", id, "error", err)
			}
		}
	}
}

// closePings stops taking ping times and waits for the queued ones.
func (s *Server) closePings() {
	s.pingMu.Lock()
	s.pingsClosed = true
	s.pingMu.Unlock()
	s.pingWG.Wait()
}

// resume takes over the session of a repeater that logged in to another
// DMRHub replica from the same address, as the KV store records it, so
// that a rolling restart does not make the repeater log in again.
func (s *Server) resume(ctx context.Context, now time.Duration, l *link, data []byte, record recordFunc) {
	if !resumable(data, l.dialect) {
		return
	}
	id, ok := dmrd.RepeaterID(data, l.dialect)
	if !ok {
		return
	}
	rec, found := record(id)
	repeater := rec.repeater
	if !found || !rec.current || rec.rekeys != s.rekeys[id] ||
		repeater.Connection != models.RepeaterStateConnected || storedDialect(repeater) != l.dialect {
		return
	}
	moved := !l.ep.matchesKV(repeater.IP, repeater.Port)
	if moved && !rec.moved {
		return
	}
	s.resumeLink(ctx, now, l, id)
	if s.byID[id] != l {
		return
	}
	l.moved = moved
	ip, port := l.ep.kvAddr()
	s.records.Do(uint(id), func() {
		own := repeater
		own.IP, own.Port = ip, port
		s.keepOwnRecord(uint(id), own)
	})
	if moved {
		s.records.Do(uint(id), func() {
			repeater, err := s.kvClient.GetRepeater(ctx, uint(id))
			if err != nil {
				return
			}
			repeater.IP, repeater.Port = ip, port
			s.kvClient.StoreRepeater(ctx, uint(id), repeater)
			s.keepOwnRecord(uint(id), repeater)
		})
	}
}

// moved reports whether the repeater id, whose KV record holds another
// address, moved to ep in a rolling restart, so that the packet data from
// ep may resume its session. A load balancer in front of the replicas,
// such as the UDP proxy of Envoy Gateway, sends the packets of a repeater
// from a new port when it moves the repeater away from a replica that
// drains, so the record cannot match. The move is taken only from the IP
// of the record, only while the replica that holds the session drains,
// and by one replica only; a second address that claims the repeater then
// finds a replica serving it and must log in. It runs off the listener,
// before the packet is handled.
func (s *Server) moved(ctx context.Context, ep endpoint, data []byte, repeater models.Repeater, id uint32) bool {
	d := storedDialect(repeater)
	if s.hub == nil || repeater.Connection != models.RepeaterStateConnected || ep.matchesKV(repeater.IP, repeater.Port) ||
		!resumable(data, d) || dmrd.Classify(data, d) == dmrd.CommandRPTO {
		return false
	}
	ip, port := ep.kvAddr()
	if port == 0 || repeater.Port == 0 || ip != repeater.IP {
		return false
	}
	if !s.hub.TakeOver(ctx, uint(id)) {
		return false
	}
	slog.Info("Repeater moved from a draining replica", "repeaterID", id, "from", fmt.Sprintf("%s:%d", repeater.IP, repeater.Port), "to", ep.String())
	return true
}

func (s *Server) resumeLink(ctx context.Context, now time.Duration, l *link, id uint32) {
	slog.Info("Resuming repeater session", "repeaterID", id, "remoteAddr", l.ep.String())
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	s.run(ctx, now, l, l.m.Resume(now, id, buf[:0]))
}

// dropMoved ends the session of the repeater id here, without a word to the
// repeater, when another replica claims it: the repeater moved there, or
// logged in there again.
// It first stops the packets of the hub to the repeater and takes those
// already on their way, so that the handover holds every packet that went
// out after the move, and those that ruleFor holds of the ones before.
func (s *Server) dropMoved(id uint, since time.Time) {
	if id > math.MaxUint32 || s.hub == nil {
		return
	}
	s.mu.Lock()
	l, ok := s.byID[uint32(id)]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.hub.StopFor(models.RepeaterTypeMMDVM, id)
	s.handOver(id, since, ruleFor(l.ep))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID[uint32(id)] != l {
		return
	}
	slog.Info("Repeater session moved to another replica", "repeaterID", id, "remoteAddr", l.ep.String())
	s.dropOwnRecord(id)
	s.stopBeacon(l)
	delete(s.byID, uint32(id))
	s.connected.Delete(id)
	l.m.Reset()
	s.forget(l)
}

// endSessions ends every session of the repeater id here with MSTCL, as
// after a change of its password, so that it must log in again with the
// new one. It also ends a login that is still going on, which may have
// read the old password.
func (s *Server) endSessions(ctx context.Context, id uint) {
	if id > math.MaxUint32 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rekeys == nil {
		s.rekeys = map[uint32]uint64{}
	}
	s.rekeys[uint32(id)]++
	if s.stopped.Load() {
		return
	}
	now := s.now()
	for _, l := range s.links {
		if l.m.ID() != uint32(id) || l.m.State() == dmrdsession.StateIdle {
			continue
		}
		slog.Info("Ending the session of a repeater whose password changed", "repeaterID", id, "remoteAddr", l.ep.String())
		var buf [dmrdsession.MaxOutputs]dmrdsession.Output
		s.run(ctx, now, l, l.m.Close(now, buf[:0]))
		s.forget(l)
	}
}

// mayResumeForEgress reports whether resumeForEgress may try the repeater
// id now.
func (s *Server) mayResumeForEgress(id uint) bool {
	if id > math.MaxUint32 || s.stopped.Load() {
		return false
	}
	at, ok := s.egressMiss.Load(id)
	return !ok || time.Since(at) >= egressResumeRetry
}

// resumeForEgress resumes the session of a repeater that the hub has a
// packet for and that this server has no session of, as after a handoff,
// when the KV store records it as connected and the hub of this server
// owns it. It returns the address of the repeater. Without it, a repeater
// that only listens would get nothing until its next ping.
func (s *Server) resumeForEgress(ctx context.Context, id uint) (endpoint, bool) {
	if id > math.MaxUint32 || !s.mayResumeForEgress(id) {
		return endpoint{}, false
	}
	s.mu.Lock()
	rekeys := s.rekeys[uint32(id)]
	s.mu.Unlock()
	repeater, err := s.kvClient.GetRepeater(ctx, id)
	if kvDown(err) {
		if own, ok := s.ownRecord(id); ok {
			repeater, err = own, nil
		}
	}
	if err != nil || repeater.Connection != models.RepeaterStateConnected || repeater.Port == 0 ||
		(s.hub != nil && !s.hub.OwnsRepeater(ctx, id)) || !s.loggedInWithCurrentSecret(id, repeater) {
		s.egressMiss.Store(id, time.Now())
		return endpoint{}, false
	}
	ep := udpEndpoint(net.UDPAddr{IP: net.ParseIP(repeater.IP), Port: repeater.Port})
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.connected.Load(id); ok {
		return a, true
	}
	l, err := s.linkAt(ep, storedDialect(repeater))
	if err != nil || l.m.State() != dmrdsession.StateIdle || l.dialect != storedDialect(repeater) || rekeys != s.rekeys[uint32(id)] {
		s.egressMiss.Store(id, time.Now())
		return endpoint{}, false
	}
	s.resumeLink(ctx, s.now(), l, uint32(id))
	s.forget(l)
	a, ok := s.connected.Load(id)
	if !ok {
		s.egressMiss.Store(id, time.Now())
	}
	return a, ok
}

// ownRecordTTL is how long a record that this replica keeps of a session
// lasts after its last ping, as long as the KV store keeps one.
const ownRecordTTL = 5 * time.Minute

// kvDown reports whether err is a failure of the KV store itself, not a
// record that is missing or bad.
func kvDown(err error) bool {
	return err != nil && !errors.Is(err, servers.ErrNoSuchRepeater) && !errors.Is(err, servers.ErrUnmarshalRepeater)
}

// keepOwnRecord keeps rec as this replica's record of the session of the
// repeater id, which a resume takes in place of the KV record while the KV
// store is down.
func (s *Server) keepOwnRecord(id uint, rec models.Repeater) {
	s.ownMu.Lock()
	defer s.ownMu.Unlock()
	if s.ownRecords == nil {
		s.ownRecords = map[uint]models.Repeater{}
	}
	s.ownRecords[id] = rec
}

func (s *Server) dropOwnRecord(id uint) {
	s.ownMu.Lock()
	defer s.ownMu.Unlock()
	delete(s.ownRecords, id)
}

// ownRecord returns this replica's record of the session of the repeater
// id, while it would still be in the KV store.
func (s *Server) ownRecord(id uint) (models.Repeater, bool) {
	s.ownMu.Lock()
	defer s.ownMu.Unlock()
	rec, ok := s.ownRecords[id]
	if ok && time.Since(rec.LastPing) >= ownRecordTTL {
		delete(s.ownRecords, id)
		return models.Repeater{}, false
	}
	return rec, ok
}

// secretSaltSize is the size of the random salt of a secret check.
const secretSaltSize = 16

// secretCheck returns a check of password for the KV record of a session
// that logged in with it: a random salt and the HMAC-SHA256 of password
// under it, no more than the RPTK of the login shows.
func secretCheck(password string) ([]byte, error) {
	salt := make([]byte, secretSaltSize, secretSaltSize+sha256.Size)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to read random salt: %w", err)
	}
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(password))
	return mac.Sum(salt), nil
}

// loggedInWithCurrentSecret reports whether the KV record rec of the
// repeater id came from a login with the password that the repeater has
// now, so that a session that logged in with an old one never resumes. A
// record without a check, from an older DMRHub, is not: that hotspot logs in
// again.
func (s *Server) loggedInWithCurrentSecret(id uint, rec models.Repeater) bool {
	if s.DB == nil || len(rec.SecretCheck) != secretSaltSize+sha256.Size {
		return false
	}
	password, err := s.password(id)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, rec.SecretCheck[:secretSaltSize])
	_, _ = mac.Write([]byte(password))
	return hmac.Equal(mac.Sum(nil), rec.SecretCheck[secretSaltSize:])
}
