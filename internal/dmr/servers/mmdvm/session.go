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
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"go.opentelemetry.io/otel"
)

// beaconDelay is the time after a login at which the server asks the
// repeater for a beacon.
const beaconDelay = time.Minute

// maxPending is the most sessions that are not logged in. LoginTimeout ends
// each of them, but RPTLs from many spoofed addresses could still fill the
// session table faster than that.
const maxPending = 1024

var errTooManyPending = errors.New("too many sessions that are not logged in")

// link is the session of the repeater at one address.
type link struct {
	key    string
	addr   net.UDPAddr
	m      *dmrdsession.Master
	beacon *time.Timer
}

func (s *Server) now() time.Duration {
	if s.epoch.IsZero() {
		s.epoch = time.Now()
	}
	return time.Since(s.epoch)
}

func (s *Server) linkFor(addr net.UDPAddr) (*link, error) {
	if s.links == nil {
		s.links = map[string]*link{}
		s.byID = map[uint32]*link{}
	}
	key := addr.String()
	if l, ok := s.links[key]; ok {
		return l, nil
	}
	if len(s.links)-len(s.byID) >= maxPending {
		return nil, errTooManyPending
	}
	m, err := dmrdsession.NewMaster(&dmrdsession.MasterConfig{
		Dialect:      dmrd.DialectMMDVM,
		LoginTimeout: dmrdsession.LoginTimeoutDefault,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to make MMDVM session: %w", err)
	}
	l := &link{key: key, addr: addr, m: m}
	s.links[key] = l
	return l, nil
}

func (s *Server) forget(l *link) {
	if l.m.State() == dmrdsession.StateIdle && s.links[l.key] == l {
		delete(s.links, l.key)
	}
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
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
	l, err := s.linkFor(remoteAddr)
	if err != nil {
		slog.Error("Error making session", "remoteAddr", remoteAddr.String(), "error", err)
		return
	}
	now := s.now()
	if l.m.State() == dmrdsession.StateIdle {
		s.resume(ctx, now, l, data)
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

func (s *Server) write(addr net.UDPAddr, p []byte) {
	if _, err := s.Server.WriteToUDP(p, &addr); err != nil {
		slog.Error("Error sending packet", "remoteAddr", addr.String(), "error", err)
	}
}

// run carries out the outputs of a session. A config that DMRHub cannot
// read refuses the login, so the acknowledgement that the session made for
// it is not sent.
func (s *Server) run(ctx context.Context, now time.Duration, l *link, out []dmrdsession.Output) {
	for _, o := range out {
		if o.Kind == dmrdsession.OutputConfig && !s.takeConfig(ctx, l, o.Packet) {
			var buf [dmrdsession.MaxOutputs]dmrdsession.Output
			s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
			return
		}
	}
	for _, o := range out {
		switch o.Kind {
		case dmrdsession.OutputSend:
			s.write(l.addr, o.Packet)
		case dmrdsession.OutputLogin:
			s.login(ctx, now, l, o.ID)
		case dmrdsession.OutputUp:
			s.up(ctx, l, o.ID)
		case dmrdsession.OutputDown:
			s.down(ctx, l, o)
		case dmrdsession.OutputOptions:
			slog.Info("Repeater options", "repeaterID", o.ID, "options", string(o.Packet))
		case dmrdsession.OutputData:
			s.data(ctx, o)
		case dmrdsession.OutputMessage:
			slog.Debug("Repeater message", "repeaterID", o.ID, "command", o.Command)
		case dmrdsession.OutputPong:
			s.ping(ctx, l)
		case dmrdsession.OutputConfig:
			var buf [dmrdsession.MaxOutputs]dmrdsession.Output
			s.run(ctx, now, l, l.m.Accept(now, buf[:0]))
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

func (s *Server) login(ctx context.Context, now time.Duration, l *link, id uint32) {
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	password, err := s.password(uint(id))
	if err != nil {
		slog.Debug("Refusing login", "repeaterID", id, "remoteAddr", l.addr.String(), "error", err)
		s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
		return
	}
	salt, err := s.salt()
	if err != nil {
		slog.Error("Error generating random salt", "error", err)
		s.run(ctx, now, l, l.m.Refuse(now, buf[:0]))
		return
	}
	slog.Debug("Login from Repeater ID", "repeaterID", id)
	s.run(ctx, now, l, l.m.Admit(now, salt, []byte(password), buf[:0]))
}

var errNoPassword = errors.New("repeater has no password")

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
	if repeater.Password == "" {
		return "", errNoPassword
	}
	return repeater.Password, nil
}

func (s *Server) takeConfig(ctx context.Context, l *link, data []byte) bool {
	repeaterID := uint(l.m.ID())
	repeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return false
	}
	if err := repeater.ParseConfig(data, s.Version, s.Commit); err != nil {
		slog.Error("Invalid RPTC packet", "repeaterID", repeaterID, "error", err)
		return false
	}
	now := time.Now()
	repeater.IP = l.addr.IP.String()
	repeater.Port = l.addr.Port
	repeater.Connected = now
	repeater.LastPing = now
	repeater.Connection = models.RepeaterStateConnected

	dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return false
	}
	dbRepeater.UpdateFrom(repeater)
	if err := s.DB.Save(&dbRepeater).Error; err != nil {
		slog.Error("Error saving repeater to database", "repeaterID", repeaterID, "error", err)
		return false
	}
	s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
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
	s.connected.Store(uint(id), l.addr)
	s.stopBeacon(l)
	addr := l.addr
	l.beacon = time.AfterFunc(beaconDelay, func() {
		if a, ok := s.connected.Load(uint(id)); !ok || a.String() != addr.String() {
			return
		}
		p, err := (&dmrd.Control{Command: dmrd.CommandRPTSBKN, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
		if err == nil {
			s.write(addr, p)
		}
	})
	if s.hub != nil {
		s.hub.ActivateRepeater(ctx, uint(id))
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
	delete(s.byID, o.ID)
	s.connected.Delete(uint(o.ID))
	if (o.Reason != dmrdsession.ReasonTimeout || s.kvStale(ctx, o.ID)) && !s.kvClient.DeleteRepeater(ctx, uint(o.ID)) {
		slog.Error("Repeater ID not deleted", "repeaterID", o.ID)
	}
	if s.hub != nil {
		s.hub.DeactivateRepeater(ctx, uint(o.ID))
	}
}

// kvStale reports whether the KV record of the repeater id has no ping
// within the session timeout. A fresh record after a timeout here means
// that another replica took the session over and keeps the record.
func (s *Server) kvStale(ctx context.Context, id uint32) bool {
	repeater, err := s.kvClient.GetRepeater(ctx, uint(id))
	return err != nil || time.Since(repeater.LastPing) >= dmrdsession.TimeoutDefault
}

func (s *Server) data(ctx context.Context, o dmrdsession.Output) {
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
			s.hub.RoutePacket(ctx, packet, models.RepeaterTypeMMDVM)
		}
	case dmrd.CommandDMRA:
		alias, err := dmrd.DecodeTalkerAlias(o.Packet)
		if err != nil {
			slog.Error("Invalid DMRA packet", "length", len(o.Packet), "error", err)
			return
		}
		slog.Debug("Talk alias", "repeaterID", alias.Repeater, "type", alias.Type, "data", string(alias.Data[:]))
	default:
		slog.Debug("Repeater data", "repeaterID", o.ID, "command", o.Command)
	}
}

func (s *Server) ping(ctx context.Context, l *link) {
	repeaterID := uint(l.m.ID())
	now := time.Now()
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from kv", "repeaterID", repeaterID, "error", err)
		return
	}
	repeater.PingsReceived++
	repeater.LastPing = now
	repeater.IP = l.addr.IP.String()
	repeater.Port = l.addr.Port
	s.kvClient.StoreRepeater(ctx, repeaterID, repeater)

	dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return
	}
	dbRepeater.LastPing = now
	if err := s.DB.Save(&dbRepeater).Error; err != nil {
		slog.Error("Error saving repeater", "repeaterID", repeaterID, "error", err)
	}
}

// resume takes over the session of a repeater that logged in to another
// DMRHub replica from the same address, as the KV store records it, so
// that a rolling restart does not make the repeater log in again.
func (s *Server) resume(ctx context.Context, now time.Duration, l *link, data []byte) {
	switch dmrd.Classify(data, dmrd.DialectMMDVM) { //nolint:exhaustive
	case dmrd.CommandDMRD, dmrd.CommandDMRA, dmrd.CommandDMRG, dmrd.CommandRPTPING, dmrd.CommandRPTO:
	default:
		return
	}
	id, ok := dmrd.RepeaterID(data, dmrd.DialectMMDVM)
	if !ok {
		return
	}
	repeater, err := s.kvClient.GetRepeater(ctx, uint(id))
	if err != nil || repeater.Connection != models.RepeaterStateConnected ||
		repeater.IP != l.addr.IP.String() || repeater.Port != l.addr.Port {
		return
	}
	slog.Info("Resuming repeater session", "repeaterID", id, "remoteAddr", l.addr.String())
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	s.run(ctx, now, l, l.m.Resume(now, id, buf[:0]))
}
