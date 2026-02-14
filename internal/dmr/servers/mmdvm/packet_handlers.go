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
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"go.opentelemetry.io/otel"
)

const max32Bit = 0xFFFFFFFF

func (s *Server) validRepeater(ctx context.Context, repeaterID uint, connection models.RepeaterState) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.validRepeater")
	defer span.End()
	valid := true
	if !s.kvClient.RepeaterExists(ctx, repeaterID) {
		slog.Error("Repeater does not exist", "repeaterID", repeaterID)
		valid = false
	}
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from kv", "repeaterID", repeaterID, "error", err)
		valid = false
	}
	if repeater.Connection != connection {
		slog.Error("Repeater state does not match expected", "repeaterID", repeaterID, "repeaterState", repeater.Connection, "expectedState", connection)
		valid = false
	}
	return valid
}

func (s *Server) handleDMRAPacket(ctx context.Context, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRAPacket")
	defer span.End()

	const dmrALength = 15
	if len(data) < dmrALength {
		slog.Error("Invalid packet length", "length", len(data))
		return
	}

	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	slog.Debug("DMR talk alias from Repeater ID", "repeaterIDBytes", repeaterIDBytes)
	if s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		s.kvClient.UpdateRepeaterPing(ctx, repeaterID)
		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			// Repeater not found, drop
			slog.Error("Repeater not found in DB", "repeaterID", repeaterID)
			return
		}
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			slog.Error("Error saving repeater", "error", err)
			return
		}

		typeBytes := data[8:9]
		// Type can be 0 for a full talk alias, or 1,2,3 for talk alias blocks
		slog.Debug("Talk alias type", "type", typeBytes[0])

		// data is the next 7 bytes
		data := string(data[9:16])
		// This is the talker alias
		slog.Debug("Talk alias data", "data", data)

		// What to do with this?
	}
}

func (s *Server) handleDMRDPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRDPacket")
	defer span.End()

	// Validate packet length
	if !s.isValidDMRDPacketLength(data) {
		return
	}

	repeaterID := s.extractRepeaterID(data)
	slog.Debug("DMR Data from Repeater ID", "repeaterID", repeaterID)

	if !s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		return
	}

	// Update repeater ping and database
	if err := s.updateRepeaterPing(ctx, repeaterID); err != nil {
		return
	}

	// Unpack and validate packet
	packet, ok := models.UnpackPacket(data)
	if !ok {
		slog.Error("Failed to unpack packet from repeater", "repeaterID", repeaterID)
		return
	}

	if packet.Dst == 0 {
		return
	}

	slog.Debug("DMRD packet received", "packet", packet.String(), "remoteAddr", remoteAddr.String())

	s.hub.RoutePacket(ctx, packet, models.RepeaterTypeMMDVM)
}

func (s *Server) isValidDMRDPacketLength(data []byte) bool {
	// DMRD packets are either 53 or 55 bytes long
	if len(data) != 53 && len(data) != 55 {
		slog.Error("Invalid DMRD packet length", "length", len(data))
		return false
	}
	return true
}

func (s *Server) extractRepeaterID(data []byte) uint {
	repeaterIDBytes := data[11:15]
	return uint(binary.BigEndian.Uint32(repeaterIDBytes))
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

func (s *Server) handleRPTLPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTLPacket")
	defer span.End()

	// RPTL packets are 8 bytes long
	const rptlLen = 8
	if len(data) != rptlLen {
		slog.Error("Invalid RPTL packet length", "length", len(data))
		return
	}
	repeaterIDBytes := data[4:]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
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
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
		slog.Debug("Repeater ID is not valid, sending NAK", "repeaterID", repeaterID)
	} else {
		repeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			slog.Error("Error finding repeater", "error", err)
			return
		}

		bigSalt, err := rand.Int(rand.Reader, big.NewInt(max32Bit))
		if err != nil {
			slog.Error("Error generating random salt", "error", err)
		}
		// Since we're generating from [0, max32Bit), this conversion is safe
		// but we'll add explicit bounds checking to satisfy gosec
		saltUint64 := bigSalt.Uint64()
		if saltUint64 <= 0xFFFFFFFF {
			repeater.Salt = uint32(saltUint64)
		} else {
			// This should never happen given our max32Bit constant, but handle it just in case
			slog.Error("Generated salt value exceeds uint32 range", "saltValue", saltUint64)
			repeater.Salt = 0xFFFFFFFF
		}
		repeater.IP = remoteAddr.IP.String()
		repeater.Port = remoteAddr.Port
		repeater.Connection = models.RepeaterStateLoginReceived
		repeater.LastPing = time.Now()
		repeater.Connected = time.Now()
		s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
		// bigSalt.Bytes() can be less than 4 bytes, so we need make sure we prefix 0s
		var saltBytes [4]byte
		if len(bigSalt.Bytes()) < len(saltBytes) {
			copy(saltBytes[len(saltBytes)-len(bigSalt.Bytes()):], bigSalt.Bytes())
		} else {
			copy(saltBytes[:], bigSalt.Bytes())
		}
		s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, saltBytes[:])
		s.kvClient.UpdateRepeaterConnection(ctx, repeaterID, models.RepeaterStateChallengeSent)
	}
}

func (s *Server) handleRPTKPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTKPacket")
	defer span.End()

	// RPTK packets are 8 bytes long + a 32 byte sha256 hash
	const rptkLen = 40
	if len(data) != rptkLen {
		slog.Error("Invalid RPTK packet length", "length", len(data))
		return
	}
	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	slog.Debug("Challenge Response from Repeater ID", "repeaterID", repeaterID)
	if s.validRepeater(ctx, repeaterID, models.RepeaterStateChallengeSent) {
		var password string
		var dbRepeater models.Repeater

		repeaterExists, err := models.RepeaterIDExists(s.DB, repeaterID)
		if err != nil {
			slog.Error("Error checking if repeater exists", "error", err)
			return
		}

		if repeaterExists {
			dbRepeater, err = models.FindRepeaterByID(s.DB, repeaterID)
			if err != nil {
				slog.Error("Error finding repeater", "error", err)
				return
			}
			password = dbRepeater.Password
		} else {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			slog.Debug("Repeater ID does not exist in db, sending NAK", "repeaterID", repeaterID)
			return
		}

		if password == "" {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			slog.Debug("Repeater ID did not provide password, sending NAK", "repeaterID", repeaterID)
			return
		}

		s.kvClient.UpdateRepeaterPing(ctx, repeaterID)
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			slog.Error("Error saving repeater to db", "error", err)
			return
		}

		repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
		if err != nil {
			slog.Error("Error getting repeater from kv", "error", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			slog.Debug("Repeater not found in kv", "repeaterID", repeaterID, "remoteAddr", remoteAddr.String())
		}
		rxSalt := binary.BigEndian.Uint32(data[8:])
		// sha256 hash repeater.Salt + the passphrase
		const bytesIn32Bits = 4
		saltBytes := make([]byte, bytesIn32Bits)
		binary.BigEndian.PutUint32(saltBytes, repeater.Salt)
		hash := sha256.Sum256(append(saltBytes, []byte(password)...))
		calcedSalt := binary.BigEndian.Uint32(hash[:])
		if calcedSalt == rxSalt {
			slog.Info("Repeater ID authed, sending ACK", "repeaterID", repeaterID)
			s.kvClient.UpdateRepeaterConnection(ctx, repeaterID, models.RepeaterStateWaitingConfig)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, repeaterIDBytes)
			go func() {
				time.Sleep(1 * time.Second)
				s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTSBKN, repeaterIDBytes)
			}()
		} else {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
		}
	} else {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
}

func (s *Server) handleRPTCLPacket(ctx context.Context, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCLPacket")
	defer span.End()

	// RPTCL packets are 8 bytes long
	const rptclLen = 9
	if len(data) != rptclLen {
		slog.Error("Invalid RPTCL packet length", "length", len(data))
		return
	}
	repeaterIDBytes := data[5:]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	slog.Debug("Disconnect from Repeater ID", "repeaterID", repeaterID)
	if s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
	if !s.kvClient.DeleteRepeater(ctx, repeaterID) {
		slog.Error("Repeater ID not deleted", "repeaterID", repeaterID)
	}
	s.connected.Delete(repeaterID)
}

func (s *Server) handleRPTCPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCPacket")
	defer span.End()

	// RPTC packets are 302 bytes long
	const rptcLen = 302
	if len(data) != rptcLen {
		slog.Error("Invalid RPTC packet length", "length", len(data))
		return
	}
	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	slog.Debug("Config from repeater", "repeaterID", repeaterID, "remoteAddr", remoteAddr.String())

	if s.validRepeater(ctx, repeaterID, models.RepeaterStateWaitingConfig) {
		s.kvClient.UpdateRepeaterPing(ctx, repeaterID)
		repeater, err := s.kvClient.GetRepeater(ctx, repeaterID)
		if err != nil {
			slog.Error("Error getting repeater from kv", "error", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}

		err = repeater.ParseConfig(data, s.Version, s.Commit)
		if err != nil {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}

		repeater.Connected = time.Now()
		repeater.LastPing = time.Now()
		repeater.Connection = models.RepeaterStateConnected

		s.kvClient.StoreRepeater(ctx, repeaterID, repeater)
		s.connected.Store(repeaterID, struct{}{})
		slog.Info("Repeater connected", "repeaterID", repeaterID, "callsign", repeater.Callsign)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, repeaterIDBytes)
		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			slog.Error("Error finding repeater", "error", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}
		dbRepeater.UpdateFrom(repeater)
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			slog.Error("Error saving repeater to database", "error", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}
	} else {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
}

func (s *Server) handleRPTPINGPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTPINGPacket")
	defer span.End()

	// RPTP packets are 11 bytes long
	const rptpLength = 11
	if len(data) != rptpLength {
		slog.Error("Invalid RPTP packet length", "length", len(data))
		return
	}
	repeaterIDBytes := data[7:]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	slog.Debug("Ping from repeater", "repeaterID", repeaterID, "remoteAddr", remoteAddr.String())

	if s.validRepeater(ctx, repeaterID, models.RepeaterStateConnected) {
		s.kvClient.UpdateRepeaterPing(ctx, repeaterID)
		// Track this repeater as locally connected. In multi-replica Kubernetes
		// deployments, repeaters may be routed to a new pod without re-doing the
		// full RPTL/RPTK/RPTC handshake. By tracking on every ping we ensure
		// Stop() can send MSTCL to all repeaters we're actually serving.
		s.connected.Store(repeaterID, struct{}{})
		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			// No repeater found, drop
			slog.Error("No repeater found for ID", "repeaterID", repeaterID)
			return
		}
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			slog.Error("Error saving repeater to database", "error", err)
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
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTPONG, repeaterIDBytes)
	} else {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
}
