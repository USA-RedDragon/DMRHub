// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

package hbrp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"go.opentelemetry.io/otel"
)

const parrotDelay = 3 * time.Second
const max32Bit = 0xFFFFFFFF

func (s *Server) validRepeater(ctx context.Context, repeaterID uint, connection string, remoteAddr net.UDPAddr) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.validRepeater")
	defer span.End()
	valid := true
	if !s.Redis.RepeaterExists(ctx, repeaterID) {
		logging.Errorf("Repeater %d does not exist", repeaterID)
		valid = false
	}
	repeater, err := s.Redis.GetRepeater(ctx, repeaterID)
	if err != nil {
		logging.Errorf("Error getting repeater %d from redis", repeaterID)
		valid = false
	}
	if repeater.IP != remoteAddr.IP.String() {
		logging.Errorf("Repeater %d IP %s does not match remote %s", repeaterID, repeater.IP, remoteAddr.IP.String())
		valid = false
	}
	if repeater.Connection != connection {
		logging.Errorf("Repeater %d state %s does not match expected %s", repeaterID, repeater.Connection, connection)
		valid = false
	}
	return valid
}

func (s *Server) switchDynamicTalkgroup(ctx context.Context, packet models.Packet) {
	// If the source repeater's (`packet.Repeater`) database entry's
	// `TS1DynamicTalkgroupID` or `TS2DynamicTalkgroupID` (respective
	// of the current `packet.Slot`) doesn't match the packet's `Dst`
	// field, then we need to update the database entry to reflect
	// the new dynamic talkgroup on the appropriate slot.

	_, span := otel.Tracer("DMRHub").Start(ctx, "Server.switchDynamicTalkgroup")
	defer span.End()

	repeaterExists, err := models.RepeaterIDExists(s.DB, packet.Repeater)
	if err != nil {
		logging.Errorf("Error checking if repeater %d exists: %s", packet.Repeater, err.Error())
		return
	}

	if !repeaterExists {
		logging.Logf("Repeater %d not found in DB", packet.Repeater)
		return
	}

	talkgroupExists, err := models.TalkgroupIDExists(s.DB, packet.Dst)
	if err != nil {
		logging.Errorf("Error checking if talkgroup %d exists: %s", packet.Dst, err.Error())
		return
	}

	if !talkgroupExists {
		logging.Logf("Talkgroup %d not found in DB", packet.Dst)
		return
	}

	repeater, err := models.FindRepeaterByID(s.DB, packet.Repeater)
	if err != nil {
		logging.Errorf("Error finding repeater %d: %s", packet.Repeater, err.Error())
		return
	}

	talkgroup, err := models.FindTalkgroupByID(s.DB, packet.Dst)
	if err != nil {
		logging.Errorf("Error finding talkgroup %d: %s", packet.Dst, err.Error())
		return
	}
	if packet.Slot {
		if repeater.TS2DynamicTalkgroupID == nil || *repeater.TS2DynamicTalkgroupID != packet.Dst {
			logging.Logf("Dynamically Linking %d timeslot 2 to %d", packet.Repeater, packet.Dst)
			repeater.TS2DynamicTalkgroup = talkgroup
			repeater.TS2DynamicTalkgroupID = &packet.Dst
			go GetSubscriptionManager(s.DB).ListenForCallsOn(s.Redis.Redis, repeater.ID, packet.Dst) //nolint:golint,contextcheck
			err := s.DB.Save(&repeater).Error
			if err != nil {
				logging.Errorf("Error saving repeater: %s", err.Error())
			}
		}
	} else {
		if repeater.TS1DynamicTalkgroupID == nil || *repeater.TS1DynamicTalkgroupID != packet.Dst {
			logging.Logf("Dynamically Linking %d timeslot 1 to %d", packet.Repeater, packet.Dst)
			repeater.TS1DynamicTalkgroup = talkgroup
			repeater.TS1DynamicTalkgroupID = &packet.Dst
			go GetSubscriptionManager(s.DB).ListenForCallsOn(s.Redis.Redis, repeater.ID, packet.Dst) //nolint:golint,contextcheck
			err := s.DB.Save(&repeater).Error
			if err != nil {
				logging.Errorf("Error saving repeater: %s", err.Error())
			}
		}
	}
}

func (s *Server) handleDMRAPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRAPacket")
	defer span.End()

	const dmrALength = 15
	if len(data) < dmrALength {
		logging.Errorf("Invalid packet length: %d", len(data))
		return
	}

	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	logging.Logf("DMR talk alias from Repeater ID: %d", repeaterIDBytes)
	if s.validRepeater(ctx, repeaterID, "YES", remoteAddr) {
		s.Redis.UpdateRepeaterPing(ctx, repeaterID)
		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			// Repeater not found, drop
			logging.Errorf("Repeater %d not found in DB", repeaterID)
			return
		}
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			logging.Errorf("Error saving repeater: %s", err.Error())
			return
		}

		typeBytes := data[8:9]
		// Type can be 0 for a full talk alias, or 1,2,3 for talk alias blocks
		logging.Logf("Talk alias type: %d", typeBytes[0])

		// data is the next 7 bytes
		data := string(data[9:16])
		// This is the talker alias
		logging.Logf("Talk alias data: %s", data)

		// What to do with this?
	}
}

func (s *Server) TrackCall(ctx context.Context, packet models.Packet, isVoice bool) {
	// Don't call track unlink
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.TrackCall")
	defer span.End()

	if packet.Dst != 4000 && isVoice {
		if !s.CallTracker.IsCallActive(ctx, packet) {
			s.CallTracker.StartCall(ctx, packet)
		}
		if s.CallTracker.IsCallActive(ctx, packet) {
			s.CallTracker.ProcessCallPacket(ctx, packet)
			if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
				s.CallTracker.EndCall(ctx, packet)
			}
		}
	}
}

func (s *Server) doParrot(ctx context.Context, packet models.Packet, repeaterID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.doParrot")
	defer span.End()
	if !s.Parrot.IsStarted(ctx, packet.StreamID) {
		s.Parrot.StartStream(ctx, packet.StreamID, repeaterID)
		if config.GetConfig().Debug {
			logging.Logf("Parrot call from %d", packet.Src)
		}
	}
	s.Parrot.RecordPacket(ctx, packet.StreamID, packet)
	if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
		s.Parrot.StopStream(ctx, packet.StreamID)
		go func() {
			packets := s.Parrot.GetStream(ctx, packet.StreamID)
			time.Sleep(parrotDelay)
			// Track the duration of the call to ensure that we send out packets right on the 60ms boundary
			// This is to ensure that the DMR repeater doesn't drop the packet
			startedTime := time.Now()
			for _, pkt := range packets {
				s.sendPacket(ctx, repeaterID, pkt)
				s.TrackCall(ctx, pkt, true)
				// Calculate the time since the call started
				elapsed := time.Since(startedTime)
				const packetTiming = 60 * time.Millisecond
				// If elapsed is greater than 60ms, we're behind and need to catch up
				if elapsed > packetTiming {
					logging.Errorf("Parrot call took too long to send, elapsed: %s", elapsed)
					// Sleep for 60ms minus the difference between the elapsed time and 60ms
					time.Sleep(packetTiming - (elapsed - packetTiming))
				} else {
					// Now subtract the elapsed time from 60ms to get the true delay
					delay := packetTiming - elapsed
					time.Sleep(delay)
				}
				startedTime = time.Now()
			}
		}()
	}
}

func (s *Server) doUnlink(ctx context.Context, packet models.Packet, dbRepeater models.Repeater) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Server.doUnlink")
	defer span.End()

	if packet.Slot {
		logging.Logf("Unlinking timeslot 2 from %d", packet.Repeater)
		if dbRepeater.TS2DynamicTalkgroupID != nil {
			oldTGID := *dbRepeater.TS2DynamicTalkgroupID
			s.DB.Model(&dbRepeater).Select("TS2DynamicTalkgroupID").Updates(map[string]interface{}{"TS2DynamicTalkgroupID": nil})
			err := s.DB.Model(&dbRepeater).Association("TS2DynamicTalkgroup").Delete(&dbRepeater.TS2DynamicTalkgroup)
			if err != nil {
				logging.Errorf("Error deleting TS2DynamicTalkgroup: %s", err)
			}
			GetSubscriptionManager(s.DB).CancelSubscription(dbRepeater.ID, oldTGID, 2)
		}
	} else {
		logging.Logf("Unlinking timeslot 1 from %d", packet.Repeater)
		if dbRepeater.TS1DynamicTalkgroupID != nil {
			oldTGID := *dbRepeater.TS1DynamicTalkgroupID
			s.DB.Model(&dbRepeater).Select("TS1DynamicTalkgroupID").Updates(map[string]interface{}{"TS1DynamicTalkgroupID": nil})
			err := s.DB.Model(&dbRepeater).Association("TS1DynamicTalkgroup").Delete(&dbRepeater.TS1DynamicTalkgroup)
			if err != nil {
				logging.Errorf("Error deleting TS1DynamicTalkgroup: %s", err)
			}
			GetSubscriptionManager(s.DB).CancelSubscription(dbRepeater.ID, oldTGID, 1)
		}
	}
	err := s.DB.Save(&dbRepeater).Error
	if err != nil {
		logging.Errorf("Error saving repeater: %s", err)
	}
}

func (s *Server) doUser(ctx context.Context, packet models.Packet, packedBytes []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.doUser")
	defer span.End()

	userExists, err := models.UserIDExists(s.DB, packet.Dst)
	if err != nil {
		logging.Errorf("Error checking if user exists: %s", err)
		return
	}

	if !userExists {
		logging.Errorf("User %d does not exist", packet.Dst)
		return
	}

	user, err := models.FindUserByID(s.DB, packet.Dst)
	if err != nil {
		logging.Errorf("Error finding user: %s", err)
		return
	}

	// Query lastheard where UserID == user.ID LIMIT 1
	var lastCall models.Call
	err = s.DB.Where("user_id = ?", user.ID).Order("created_at DESC").First(&lastCall).Error
	if err != nil {
		logging.Errorf("Error querying last call for user %d: %v", user.ID, err)
	} else if lastCall.ID != 0 && s.Redis.RepeaterExists(ctx, lastCall.RepeaterID) {
		// If the last call exists and that repeater is online
		// Send the packet to the last user call's repeater
		s.Redis.Redis.Publish(ctx, fmt.Sprintf("hbrp:packets:repeater:%d", lastCall.RepeaterID), packedBytes)
	}

	// For each user repeaters
	for _, repeater := range user.Repeaters {
		// If the repeater is online and the last user call was not to this repeater
		if repeater.ID != lastCall.RepeaterID && s.Redis.RepeaterExists(ctx, lastCall.RepeaterID) {
			// Send the packet to the repeater
			s.Redis.Redis.Publish(ctx, fmt.Sprintf("hbrp:packets:repeater:%d", repeater.ID), packedBytes)
		}
	}
}

func (s *Server) handleDMRDPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRDPacket")
	defer span.End()

	// DMRD packets are either 53 or 55 bytes long
	if len(data) != 53 && len(data) != 55 {
		logging.Errorf("Invalid DMRD packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[11:15]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	logging.Logf("DMR Data from Repeater ID: %d", repeaterID)
	if s.validRepeater(ctx, repeaterID, "YES", remoteAddr) {
		s.Redis.UpdateRepeaterPing(ctx, repeaterID)

		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			logging.Errorf("Error finding repeater: %s", err)
			return
		}
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			logging.Errorf("Error saving repeater: %s", err)
			return
		}

		packet, ok := models.UnpackPacket(data)
		if !ok {
			logging.Errorf("Failed to unpack packet from repeater %d", repeaterID)
			return
		}

		if packet.Dst == 0 {
			return
		}

		if config.GetConfig().Debug {
			logging.Logf("DMRD packet: %s", packet.String())
		}

		isVoice, isData := utils.CheckPacketType(packet)

		s.TrackCall(ctx, packet, isVoice)

		if packet.Dst == dmrconst.ParrotUser && isVoice {
			s.doParrot(ctx, packet, repeaterID)
			// Don't route parrot calls
			return
		}

		if packet.Dst == 4000 && isVoice {
			s.doUnlink(ctx, packet, dbRepeater)
			return
		}

		if config.GetConfig().OpenBridgePort != 0 {
			go func() {
				// We need to send this packet to all peers except the one that sent it
				peers := models.ListPeers(s.DB)
				for _, p := range peers {
					if rules.PeerShouldEgress(s.DB, p, &packet) {
						s.sendOpenBridgePacket(ctx, p.ID, packet)
					}
				}
			}()
		}

		switch {
		case packet.GroupCall && isVoice:
			exists, err := models.TalkgroupIDExists(s.DB, packet.Dst)
			if err != nil {
				logging.Errorf("Error checking if talkgroup exists: %s", err)
				return
			}
			if !exists {
				logging.Errorf("Talkgroup %d does not exist", packet.Dst)
				return
			}
			go s.switchDynamicTalkgroup(ctx, packet)

			// We can just use redis to publish to "hbrp:packets:talkgroup:<id>"
			var rawPacket models.RawDMRPacket
			rawPacket.Data = data
			rawPacket.RemoteIP = remoteAddr.IP.String()
			rawPacket.RemotePort = remoteAddr.Port
			packedBytes, err := rawPacket.MarshalMsg(nil)
			if err != nil {
				logging.Errorf("Error marshalling raw packet: %v", err)
				return
			}
			s.Redis.Redis.Publish(ctx, fmt.Sprintf("hbrp:packets:talkgroup:%d", packet.Dst), packedBytes)
		case !packet.GroupCall && isVoice:
			// packet.Dst is either a repeater or a user
			// If it's a repeater, we need to send it to the repeater
			// If it's a user, we need to send it to the repeater that the user is connected to
			// by looking up the user in the database and iterating through their repeaters

			var rawPacket models.RawDMRPacket
			rawPacket.Data = data
			rawPacket.RemoteIP = remoteAddr.IP.String()
			rawPacket.RemotePort = remoteAddr.Port

			packedBytes, err := rawPacket.MarshalMsg(nil)
			if err != nil {
				logging.Errorf("Error marshalling raw packet: %v", err)
				return
			}

			// users have 7 digit IDs, repeaters have 6 digit IDs or 9 digit IDs
			const (
				rptIDMin     = 100000
				rptIDMax     = 999999
				hotspotIDMin = 100000000
				hotspotIDMax = 999999999
				userIDMin    = 1000000
				userIDMax    = 9999999
			)
			if (packet.Dst >= rptIDMin && packet.Dst <= rptIDMax) || (packet.Dst >= hotspotIDMin && packet.Dst <= hotspotIDMax) {
				// This is to a repeater
				exists, err := models.RepeaterIDExists(s.DB, packet.Dst)
				if err != nil {
					logging.Errorf("Error checking if repeater exists: %s", err)
				}
				if !exists {
					logging.Errorf("Repeater %d does not exist", packet.Dst)
					return
				}
				s.Redis.Redis.Publish(ctx, fmt.Sprintf("hbrp:packets:repeater:%d", packet.Dst), packedBytes)
			} else if packet.Dst >= userIDMin && packet.Dst <= userIDMax {
				exists, err := models.UserIDExists(s.DB, packet.Dst)
				if err != nil {
					logging.Errorf("Error checking if user exists: %s", err)
					return
				}
				if !exists {
					logging.Errorf("User %d does not exist", packet.Dst)
					return
				}
				s.doUser(ctx, packet, packedBytes)
			}
		case isData:
			logging.Error("Unhandled data packet type")
		default:
			logging.Error("Unhandled packet type")
		}
	}
}

func (s *Server) handleRPTOPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTOPacket")
	defer span.End()

	const rptoMin = 8
	const rptoMax = 300
	const rptoRepeaterIDOffset = 4

	if len(data) < rptoMin {
		logging.Error("RPTO packet too short")
		return
	}
	if len(data) > rptoMax {
		logging.Error("RPTO packet too long")
		return
	}

	repeaterIDBytes := data[rptoRepeaterIDOffset : rptoRepeaterIDOffset+repeaterIDLength]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))

	if s.validRepeater(ctx, repeaterID, "YES", remoteAddr) {
		s.Redis.UpdateRepeaterPing(ctx, repeaterID)

		repeaterExists, err := models.RepeaterIDExists(s.DB, repeaterID)
		if err != nil {
			logging.Errorf("Error finding repeater: %s", err)
			return
		}

		if !repeaterExists {
			logging.Error("Repeater does not exist")
			return
		}

		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			logging.Errorf("Error finding repeater: %s", err)
			return
		}
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			logging.Errorf("Error saving repeater: %s", err)
			return
		}

		// Options is a string from data[8:]
		options := string(data[8:])
		logging.Logf("Received Options from repeater %d: %s", repeaterID, options)

		// https://github.com/g4klx/MMDVMHost/blob/master/DMRplus_startup_options.md
		// Options are not yet supported
	}
}

func (s *Server) handleRPTLPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTLPacket")
	defer span.End()

	// RPTL packets are 8 bytes long
	const rptlLen = 8
	const rptlRepeaterIDOffset = 4
	if len(data) != rptlLen {
		logging.Errorf("Invalid RPTL packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[rptlRepeaterIDOffset : rptlRepeaterIDOffset+repeaterIDLength]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	logging.Logf("Login from Repeater ID: %d", repeaterID)
	exists, err := models.RepeaterIDExists(s.DB, repeaterID)
	if err != nil {
		logging.Errorf("Error finding repeater: %s", err)
		return
	}
	if !exists {
		repeater := models.Repeater{}
		repeater.ID = repeaterID
		repeater.IP = remoteAddr.IP.String()
		repeater.Port = remoteAddr.Port
		repeater.Connection = "RPTL-RECEIVED"
		repeater.LastPing = time.Now()
		repeater.Connected = time.Now()
		s.Redis.StoreRepeater(ctx, repeaterID, repeater)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
		if config.GetConfig().Debug {
			logging.Logf("Repeater ID %d is not valid, sending NAK", repeaterID)
		}
	} else {
		repeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			logging.Errorf("Error finding repeater: %s", err)
			return
		}

		bigSalt, err := rand.Int(rand.Reader, big.NewInt(max32Bit))
		if err != nil {
			logging.Errorf("Error generating random salt: %v", err)
		}
		repeater.Salt = uint32(bigSalt.Uint64())
		repeater.IP = remoteAddr.IP.String()
		repeater.Port = remoteAddr.Port
		repeater.Connection = "RPTL-RECEIVED"
		repeater.LastPing = time.Now()
		repeater.Connected = time.Now()
		s.Redis.StoreRepeater(ctx, repeaterID, repeater)
		// bigSalt.Bytes() can be less than 4 bytes, so we need make sure we prefix 0s
		var saltBytes [4]byte
		if len(bigSalt.Bytes()) < len(saltBytes) {
			copy(saltBytes[len(saltBytes)-len(bigSalt.Bytes()):], bigSalt.Bytes())
		} else {
			copy(saltBytes[:], bigSalt.Bytes())
		}
		s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, saltBytes[:])
		s.Redis.UpdateRepeaterConnection(ctx, repeaterID, "CHALLENGE_SENT")
	}
}

func (s *Server) handleRPTKPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTKPacket")
	defer span.End()

	// RPTK packets are 8 bytes long + a 32 byte sha256 hash
	const rptkLen = 40
	if len(data) != rptkLen {
		logging.Errorf("Invalid RPTK packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		logging.Logf("Challenge Response from Repeater ID: %d", repeaterID)
	}
	if s.validRepeater(ctx, repeaterID, "CHALLENGE_SENT", remoteAddr) {
		var password string
		var dbRepeater models.Repeater

		repeaterExists, err := models.RepeaterIDExists(s.DB, repeaterID)
		if err != nil {
			logging.Errorf("Error checking if repeater exists: %s", err)
			return
		}

		if repeaterExists {
			dbRepeater, err = models.FindRepeaterByID(s.DB, repeaterID)
			if err != nil {
				logging.Errorf("Error finding repeater: %s", err)
				return
			}
			password = dbRepeater.Password
		} else {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				logging.Logf("Repeater ID %d does not exist in db, sending NAK", repeaterID)
			}
			return
		}

		if password == "" {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				logging.Logf("Repeater ID %d did not provide password, sending NAK", repeaterID)
			}
			return
		}

		s.Redis.UpdateRepeaterPing(ctx, repeaterID)
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			logging.Errorf("Error saving repeater to db: %v", err)
			return
		}

		repeater, err := s.Redis.GetRepeater(ctx, repeaterID)
		if err != nil {
			logging.Errorf("Error getting repeater from redis: %v", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				logging.Logf("Repeater ID %d does not exist in redis, sending NAK", repeaterID)
			}
		}
		rxSalt := binary.BigEndian.Uint32(data[8:])
		// sha256 hash repeater.Salt + the passphrase
		const bytesIn32Bits = 4
		saltBytes := make([]byte, bytesIn32Bits)
		binary.BigEndian.PutUint32(saltBytes, repeater.Salt)
		hash := sha256.Sum256(append(saltBytes, []byte(password)...))
		calcedSalt := binary.BigEndian.Uint32(hash[:])
		if calcedSalt == rxSalt {
			logging.Logf("Repeater ID %d authed, sending ACK", repeaterID)
			s.Redis.UpdateRepeaterConnection(ctx, repeaterID, "WAITING_CONFIG")
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

func (s *Server) handleRPTCLPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCLPacket")
	defer span.End()

	// RPTCL packets are 8 bytes long
	const rptclLen = 8
	if len(data) != rptclLen {
		logging.Errorf("Invalid RPTCL packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[5:9]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	logging.Logf("Disconnect from Repeater ID: %d", repeaterID)
	if s.validRepeater(ctx, repeaterID, "YES", remoteAddr) {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
	if !s.Redis.DeleteRepeater(ctx, repeaterID) {
		logging.Errorf("Repeater ID %d not deleted", repeaterID)
	}
}

func (s *Server) handleRPTCPacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCPacket")
	defer span.End()

	// RPTC packets are 302 bytes long
	const rptcLen = 302
	if len(data) != rptcLen {
		logging.Errorf("Invalid RPTC packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		logging.Logf("Repeater config from %d", repeaterID)
	}

	if s.validRepeater(ctx, repeaterID, "WAITING_CONFIG", remoteAddr) {
		s.Redis.UpdateRepeaterPing(ctx, repeaterID)
		repeater, err := s.Redis.GetRepeater(ctx, repeaterID)
		if err != nil {
			logging.Errorf("Error getting repeater from redis: %v", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}

		err = repeater.ParseConfig(data)
		if err != nil {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}

		repeater.Connected = time.Now()
		repeater.LastPing = time.Now()
		repeater.Connection = "YES"

		s.Redis.StoreRepeater(ctx, repeaterID, repeater)
		logging.Logf("Repeater ID %d (%s) connected\n", repeaterID, repeater.Callsign)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, repeaterIDBytes)
		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			logging.Errorf("Error finding repeater: %v", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			return
		}
		dbRepeater.UpdateFromRedis(repeater)
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			logging.Errorf("Error saving repeater to database: %s", err)
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
		logging.Errorf("Invalid RPTP packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[7:11]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		logging.Logf("Ping from %d", repeaterID)
	}

	if s.validRepeater(ctx, repeaterID, "YES", remoteAddr) {
		s.Redis.UpdateRepeaterPing(ctx, repeaterID)
		dbRepeater, err := models.FindRepeaterByID(s.DB, repeaterID)
		if err != nil {
			// No repeater found, drop
			logging.Errorf("No repeater found for ID %d", repeaterID)
			return
		}
		dbRepeater.LastPing = time.Now()
		err = s.DB.Save(&dbRepeater).Error
		if err != nil {
			logging.Errorf("Error saving repeater to database: %s", err)
		}

		repeater, err := s.Redis.GetRepeater(ctx, repeaterID)
		if err != nil {
			logging.Errorf("Error getting repeater from Redis: %v", err)
			return
		}
		repeater.PingsReceived++
		s.Redis.StoreRepeater(ctx, repeaterID, repeater)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTPONG, repeaterIDBytes)
	} else {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
}
