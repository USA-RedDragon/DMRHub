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
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/auth"
	"go.opentelemetry.io/otel"
)

func (s *Server) validRepeater(ctx context.Context, repeaterID uint, connection models.RepeaterState) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.validRepeater")
	defer span.End()
	if !s.kvClient.RepeaterExists(ctx, repeaterID) {
		slog.Error("Repeater does not exist", "repeaterID", repeaterID)
		return false
	}
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from kv", "repeaterID", repeaterID, "error", err)
		return false
	}
	if repeater.Connection != connection {
		slog.Error("Repeater state does not match expected", "repeaterID", repeaterID, "repeaterState", repeater.Connection, "expectedState", connection)
		return false
	}
	return true
}

func (s *Server) handleDMRAPacket(ctx context.Context, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRAPacket")
	defer span.End()

	alias, err := dmrd.DecodeTalkerAlias(data)
	if err != nil {
		slog.Error("Invalid DMRA packet", "length", len(data), "error", err)
		return
	}

	repeaterID := uint(alias.Repeater)
	slog.Debug("DMR talk alias from Repeater ID", "repeaterID", repeaterID)
	if s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		// Type can be 0 for a full talk alias, or 1,2,3 for talk alias blocks
		slog.Debug("Talk alias", "type", alias.Type, "data", string(alias.Data[:]))
	}
}

func (s *Server) handleDMRDPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRDPacket")
	defer span.End()

	// DMRD packets are either 53 or 55 bytes long
	if len(data) != dmrconst.MMDVMPacketLength && len(data) != dmrconst.MMDVMMaxPacketLength {
		slog.Error("Invalid DMRD packet length", "length", len(data))
		return
	}
	d, err := dmrd.DecodeData(data)
	if err != nil {
		slog.Error("Failed to decode DMRD packet", "error", err)
		return
	}

	repeaterID := uint(d.Repeater)
	slog.Debug("DMR Data from Repeater ID", "repeaterID", repeaterID)

	if !s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		return
	}

	packet := models.PacketFromData(&d, len(data) > dmrconst.MMDVMPacketLength)
	if packet.Dst == 0 {
		return
	}

	slog.Debug("DMRD packet received", "packet", packet.String(), "remoteAddr", remoteAddr.String())

	s.hub.RoutePacket(ctx, packet, models.RepeaterTypeMMDVM)
}

func (s *Server) updateRepeaterPing(ctx context.Context, repeaterID uint) error {
	s.kvClient.UpdateRepeaterPing(ctx, repeaterID)

	dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "error", err)
		return fmt.Errorf("failed to find repeater %d: %w", repeaterID, err)
	}

	dbRepeater.LastPing = time.Now()
	if err := s.DB.Save(&dbRepeater).Error; err != nil {
		slog.Error("Error saving repeater", "error", err)
		return fmt.Errorf("failed to save repeater %d: %w", repeaterID, err)
	}

	return nil
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

func (s *Server) handleRPTLPacket(ctx context.Context, remoteAddr net.UDPAddr, msg dmrd.Control) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTLPacket")
	defer span.End()

	repeaterID := uint(msg.ID)
	slog.Debug("Login from Repeater ID", "repeaterID", repeaterID)
	exists, err := models.RepeaterIDExists(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "error", err)
		return
	}
	if !exists {
		repeater := models.Repeater{}
		repeater.ID = repeaterID
		repeater.IP = remoteAddr.IP.String()
		repeater.Port = remoteAddr.Port
		repeater.Connection = models.RepeaterStateLoginReceived
		repeater.LastPing = time.Now()
		repeater.Connected = time.Now()
		s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		slog.Debug("Repeater ID is not valid, sending NAK", "repeaterID", repeaterID)
		return
	}

	repeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "error", err)
		return
	}
	repeater.Salt, err = s.salt()
	if err != nil {
		slog.Error("Error generating random salt", "error", err)
	}
	repeater.IP = remoteAddr.IP.String()
	repeater.Port = remoteAddr.Port
	repeater.Connection = models.RepeaterStateLoginReceived
	repeater.LastPing = time.Now()
	repeater.Connected = time.Now()
	s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
	// The RPTACK that answers RPTL carries the salt in its ID field.
	s.sendCommand(ctx, repeaterID, dmrd.CommandRPTACK, repeater.Salt)
	s.kvClient.UpdateRepeaterConnection(ctx, repeaterID, models.RepeaterStateChallengeSent)
}

func (s *Server) handleRPTKPacket(ctx context.Context, remoteAddr net.UDPAddr, msg dmrd.Control) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTKPacket")
	defer span.End()

	repeaterID := uint(msg.ID)
	slog.Debug("Challenge Response from Repeater ID", "repeaterID", repeaterID)
	if !s.validRepeater(ctx, repeaterID, models.RepeaterStateChallengeSent) {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		return
	}

	repeaterExists, err := models.RepeaterIDExists(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error checking if repeater exists", "error", err)
		return
	}
	if !repeaterExists {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		slog.Debug("Repeater ID does not exist in db, sending NAK", "repeaterID", repeaterID)
		return
	}

	dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "error", err)
		return
	}
	if dbRepeater.Password == "" {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		slog.Debug("Repeater ID did not provide password, sending NAK", "repeaterID", repeaterID)
		return
	}

	repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from kv", "error", err)
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		slog.Debug("Repeater not found in kv", "repeaterID", repeaterID, "remoteAddr", remoteAddr.String())
	}
	if !auth.VerifyToken(dmrd.DialectMMDVM, repeater.Salt, []byte(dbRepeater.Password), msg.Token[:]) {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		return
	}

	slog.Info("Repeater ID authed, sending ACK", "repeaterID", repeaterID)
	s.kvClient.UpdateRepeaterConnection(ctx, repeaterID, models.RepeaterStateWaitingConfig)
	s.sendCommand(ctx, repeaterID, dmrd.CommandRPTACK, msg.ID)
	go func() {
		timer := time.NewTimer(1 * time.Minute)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.sendCommand(ctx, repeaterID, dmrd.CommandRPTSBKN, msg.ID)
		}
	}()
}

func (s *Server) handleRPTCLPacket(ctx context.Context, msg dmrd.Control) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCLPacket")
	defer span.End()

	repeaterID := uint(msg.ID)
	slog.Debug("Disconnect from Repeater ID", "repeaterID", repeaterID)
	if s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
	}
	if !s.kvClient.DeleteRepeater(ctx, repeaterID) {
		slog.Error("Repeater ID not deleted", "repeaterID", repeaterID)
	}
	s.connected.Delete(repeaterID)
	// Cancel hub subscriptions for the disconnected repeater
	s.hub.DeactivateRepeater(ctx, repeaterID)
}

func (s *Server) handleRPTCPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCPacket")
	defer span.End()

	cfg, err := dmrd.ConfigOf(data)
	if err != nil {
		slog.Error("Invalid RPTC packet", "length", len(data), "error", err)
		return
	}
	id := cfg.ID()
	repeaterID := uint(id)
	slog.Debug("Config from repeater", "repeaterID", repeaterID, "remoteAddr", remoteAddr.String())

	if !s.validRepeater(ctx, repeaterID, models.RepeaterStateWaitingConfig) {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, id)
		return
	}
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from kv", "error", err)
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, id)
		return
	}

	err = repeater.ParseConfig(data, s.Version, s.Commit)
	if err != nil {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, id)
		return
	}

	repeater.Connected = time.Now()
	repeater.LastPing = time.Now()
	repeater.Connection = models.RepeaterStateConnected

	s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
	s.connected.Store(repeaterID, struct{}{})
	slog.Info("Repeater connected", "repeaterID", repeaterID, "callsign", repeater.Callsign)
	s.sendCommand(ctx, repeaterID, dmrd.CommandRPTACK, id)
	dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "error", err)
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, id)
		return
	}
	dbRepeater.UpdateFrom(repeater)
	err = s.DB.Save(&dbRepeater).Error
	if err != nil {
		slog.Error("Error saving repeater to database", "error", err)
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, id)
		return
	}
	// Activate hub subscriptions now that the repeater is connected
	s.hub.ActivateRepeater(ctx, repeaterID)
}

func (s *Server) handleRPTPINGPacket(ctx context.Context, remoteAddr net.UDPAddr, msg dmrd.Control) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTPINGPacket")
	defer span.End()

	repeaterID := uint(msg.ID)
	slog.Debug("Ping from repeater", "repeaterID", repeaterID, "remoteAddr", remoteAddr.String())

	if !s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		s.sendCommand(ctx, repeaterID, dmrd.CommandMSTNAK, msg.ID)
		return
	}
	// Track this repeater as locally connected. In multi-replica Kubernetes
	// deployments, repeaters may be routed to a new pod without re-doing the
	// full RPTL/RPTK/RPTC handshake. By tracking on every ping we ensure
	// Stop() can send MSTCL to all repeaters we're actually serving.
	s.connected.Store(repeaterID, struct{}{})
	if err := s.updateRepeaterPing(ctx, repeaterID); err != nil {
		slog.Error("failed to update repeater ping", "error", err)
		return
	}

	repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from kv", "error", err)
		return
	}
	repeater.PingsReceived++
	// Update the repeater's address in KV so that if this pod took over
	// from another (e.g. rolling deploy), MSTCL is sent to the current
	// address, not the stale one from the original RPTC handshake.
	repeater.IP = remoteAddr.IP.String()
	repeater.Port = remoteAddr.Port
	s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
	s.sendCommand(ctx, repeaterID, dmrd.CommandMSTPONG, msg.ID)
}
