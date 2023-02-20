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
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/sdk"
	"go.opentelemetry.io/otel"
	"k8s.io/klog/v2"
)

const parrotDelay = 3 * time.Second
const max32Bit = 0xFFFFFFFF

func (s *Server) validRepeater(ctx context.Context, repeaterID uint, connection string, remoteAddr net.UDPAddr) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.validRepeater")
	defer span.End()
	valid := true
	if !s.Redis.repeaterExists(ctx, repeaterID) {
		klog.Warningf("Repeater %d does not exist", repeaterID)
		valid = false
	}
	repeater, err := s.Redis.getRepeater(ctx, repeaterID)
	if err != nil {
		klog.Warningf("Error getting repeater %d from redis", repeaterID)
		valid = false
	}
	if repeater.IP != remoteAddr.IP.String() {
		klog.Warningf("Repeater %d IP %s does not match remote %s", repeaterID, repeater.IP, remoteAddr.IP.String())
		valid = false
	}
	if repeater.Connection != connection {
		klog.Warningf("Repeater %d state %s does not match expected %s", repeaterID, repeater.Connection, connection)
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

	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.switchDynamicTalkgroup")
	defer span.End()

	if models.RepeaterIDExists(s.DB, packet.Repeater) {
		if !models.TalkgroupIDExists(s.DB, packet.Dst) {
			if config.GetConfig().Debug {
				klog.Infof("Repeater %d not found in DB", packet.Repeater)
			}
			return
		}
		repeater := models.FindRepeaterByID(s.DB, packet.Repeater)
		talkgroup := models.FindTalkgroupByID(s.DB, packet.Dst)
		if packet.Slot {
			if repeater.TS2DynamicTalkgroupID == nil || *repeater.TS2DynamicTalkgroupID != packet.Dst {
				klog.Infof("Dynamically Linking %d timeslot 2 to %d", packet.Repeater, packet.Dst)
				repeater.TS2DynamicTalkgroup = talkgroup
				repeater.TS2DynamicTalkgroupID = &packet.Dst
				go GetSubscriptionManager().ListenForCallsOn(ctx, s.Redis.Redis, repeater, packet.Dst)
				s.DB.Save(&repeater)
			}
		} else {
			if repeater.TS1DynamicTalkgroupID == nil || *repeater.TS1DynamicTalkgroupID != packet.Dst {
				klog.Infof("Dynamically Linking %d timeslot 1 to %d", packet.Repeater, packet.Dst)
				repeater.TS1DynamicTalkgroup = talkgroup
				repeater.TS1DynamicTalkgroupID = &packet.Dst
				go GetSubscriptionManager().ListenForCallsOn(ctx, s.Redis.Redis, repeater, packet.Dst)
				s.DB.Save(&repeater)
			}
		}
	} else if config.GetConfig().Debug {
		klog.Infof("Repeater %d not found in DB", packet.Repeater)
	}
}

func (s *Server) handleDMRAPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRAPacket")
	defer span.End()

	const dmrALength = 15
	if len(data) < dmrALength {
		klog.Warningf("Invalid packet length: %d", len(data))
		return
	}

	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		klog.Infof("DMR talk alias from Repeater ID: %d", repeaterIDBytes)
	}
	if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
		s.Redis.updateRepeaterPing(ctx, repeaterID)
		dbRepeater := models.FindRepeaterByID(s.DB, repeaterID)
		if dbRepeater.RadioID == 0 {
			// Repeater not found, drop
			klog.Warningf("Repeater %d not found in DB", repeaterID)
			return
		}
		dbRepeater.LastPing = time.Now()
		s.DB.Save(&dbRepeater)

		typeBytes := data[8:9]
		// Type can be 0 for a full talk alias, or 1,2,3 for talk alias blocks
		klog.Infof("Talk alias type: %d", typeBytes[0])

		// data is the next 7 bytes
		data := string(data[9:16])
		// This is the talker alias
		klog.Infof("Talk alias data: %s", data)

		// What to do with this?
	}
}

func (s *Server) TrackCall(ctx context.Context, packet models.Packet, isVoice bool) {
	// Don't call track unlink
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.TrackCall")
	defer span.End()

	if packet.Dst != 4000 && isVoice {
		go func() {
			if !s.CallTracker.IsCallActive(ctx, packet) {
				s.CallTracker.StartCall(ctx, packet)
			}
			if s.CallTracker.IsCallActive(ctx, packet) {
				s.CallTracker.ProcessCallPacket(ctx, packet)
				if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
					s.CallTracker.EndCall(ctx, packet)
				}
			}
		}()
	}
}

func (s *Server) doParrot(ctx context.Context, packet models.Packet, repeaterID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.doParrot")
	defer span.End()
	if !s.Parrot.IsStarted(ctx, packet.StreamID) {
		s.Parrot.StartStream(ctx, packet.StreamID, repeaterID)
		if config.GetConfig().Debug {
			klog.Infof("Parrot call from %d", packet.Src)
		}
	}
	s.Parrot.RecordPacket(ctx, packet.StreamID, packet)
	if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
		s.Parrot.StopStream(ctx, packet.StreamID)
		go func() {
			packets := s.Parrot.GetStream(ctx, packet.StreamID)
			time.Sleep(parrotDelay)
			started := false
			// Track the duration of the call to ensure that we send out packets right on the 60ms boundary
			// This is to ensure that the DMR repeater doesn't drop the packet
			startedTime := time.Now()
			for j, pkt := range packets {
				s.sendPacket(ctx, repeaterID, pkt)

				go func() {
					if !started {
						s.CallTracker.StartCall(ctx, pkt)
						started = true
					}
					s.CallTracker.ProcessCallPacket(ctx, pkt)
					if j == len(packets)-1 {
						s.CallTracker.EndCall(ctx, pkt)
					}
				}()
				// Calculate the time since the call started
				elapsed := time.Since(startedTime)
				const packetTiming = 60 * time.Millisecond
				// If elapsed is greater than 60ms, we're behind and need to catch up
				if elapsed > packetTiming {
					klog.Warningf("Parrot call took too long to send, elapsed: %s", elapsed)
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
		klog.Infof("Unlinking timeslot 2 from %d", packet.Repeater)
		if dbRepeater.TS2DynamicTalkgroupID != nil {
			oldTGID := *dbRepeater.TS2DynamicTalkgroupID
			s.DB.Model(&dbRepeater).Select("TS2DynamicTalkgroupID").Updates(map[string]interface{}{"TS2DynamicTalkgroupID": nil})
			err := s.DB.Model(&dbRepeater).Association("TS2DynamicTalkgroup").Delete(&dbRepeater.TS2DynamicTalkgroup)
			if err != nil {
				klog.Errorf("Error deleting TS2DynamicTalkgroup: %s", err)
			}
			GetSubscriptionManager().CancelSubscription(dbRepeater, oldTGID)
		}
	} else {
		klog.Infof("Unlinking timeslot 1 from %d", packet.Repeater)
		if dbRepeater.TS1DynamicTalkgroupID != nil {
			oldTGID := *dbRepeater.TS1DynamicTalkgroupID
			s.DB.Model(&dbRepeater).Select("TS1DynamicTalkgroupID").Updates(map[string]interface{}{"TS1DynamicTalkgroupID": nil})
			err := s.DB.Model(&dbRepeater).Association("TS1DynamicTalkgroup").Delete(&dbRepeater.TS1DynamicTalkgroup)
			if err != nil {
				klog.Errorf("Error deleting TS1DynamicTalkgroup: %s", err)
			}
			GetSubscriptionManager().CancelSubscription(dbRepeater, oldTGID)
		}
	}
	s.DB.Save(&dbRepeater)
}

func (s *Server) doUser(ctx context.Context, packet models.Packet, packedBytes []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.doUser")
	defer span.End()

	// This is to a user
	// Search the database for the user
	if models.UserIDExists(s.DB, packet.Dst) {
		user := models.FindUserByID(s.DB, packet.Dst)
		// Query lastheard where UserID == user.ID LIMIT 1
		var lastCall models.Call
		s.DB.Where("user_id = ?", user.ID).Order("created_at DESC").First(&lastCall)
		if s.DB.Error != nil {
			klog.Errorf("Error querying last call for user %d: %s", user.ID, s.DB.Error)
		} else if lastCall.ID != 0 && s.Redis.repeaterExists(ctx, lastCall.RepeaterID) {
			// If the last call exists and that repeater is online
			// Send the packet to the last user call's repeater
			s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:repeater:%d", lastCall.RepeaterID), packedBytes)
		}

		// For each user repeaters
		for _, repeater := range user.Repeaters {
			// If the repeater is online and the last user call was not to this repeater
			if repeater.RadioID != lastCall.RepeaterID && s.Redis.repeaterExists(ctx, repeater.RadioID) {
				// Send the packet to the repeater
				s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:repeater:%d", repeater.RadioID), packedBytes)
			}
		}
	}
}

func (s *Server) handleDMRDPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleDMRDPacket")
	defer span.End()

	// DMRD packets are either 53 or 55 bytes long
	if len(data) != 53 && len(data) != 55 {
		klog.Warningf("Invalid DMRD packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[11:15]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		klog.Infof("DMR Data from Repeater ID: %d", repeaterID)
	}
	if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
		s.Redis.updateRepeaterPing(ctx, repeaterID)

		var dbRepeater models.Repeater
		if models.RepeaterIDExists(s.DB, repeaterID) {
			dbRepeater = models.FindRepeaterByID(s.DB, repeaterID)
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)
		} else {
			klog.Warningf("Repeater %d not found in DB", repeaterID)
			return
		}
		packet, ok := models.UnpackPacket(data)
		if !ok {
			klog.Warningf("Failed to unpack packet from repeater %d", repeaterID)
			return
		}

		if config.GetConfig().Debug {
			klog.Infof("DMRD packet: %s", packet.String())
		}

		isVoice, isData := utils.CheckPacketType(packet)

		if packet.Dst == 0 {
			return
		}

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

		switch {
		case packet.GroupCall && isVoice:
			go s.switchDynamicTalkgroup(ctx, packet)

			// We can just use redis to publish to "packets:talkgroup:<id>"
			var rawPacket models.RawDMRPacket
			rawPacket.Data = data
			rawPacket.RemoteIP = remoteAddr.IP.String()
			rawPacket.RemotePort = remoteAddr.Port
			packedBytes, err := rawPacket.MarshalMsg(nil)
			if err != nil {
				klog.Errorf("Error marshalling raw packet", err)
				return
			}
			s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:talkgroup:%d", packet.Dst), packedBytes)
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
				klog.Errorf("Error marshalling raw packet", err)
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
				s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:repeater:%d", packet.Dst), packedBytes)
			} else if packet.Dst >= userIDMin && packet.Dst <= userIDMax {
				s.doUser(ctx, packet, packedBytes)
			}
		case isData:
			klog.Warning("Unhandled data packet type")
		default:
			klog.Warning("Unhandled packet type")
		}
	}
}

func (s *Server) handleRPTOPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTOPacket")
	defer span.End()

	const rptoMin = 8
	const rptoMax = 300
	const rptoRepeaterIDOffset = 4

	if len(data) < rptoMin {
		klog.Warning("RPTO packet too short")
		return
	}
	if len(data) > rptoMax {
		klog.Warning("RPTO packet too long")
		return
	}

	repeaterIDBytes := data[rptoRepeaterIDOffset : rptoRepeaterIDOffset+repeaterIDLength]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		klog.Infof("Set options from %d", repeaterID)
	}

	if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
		s.Redis.updateRepeaterPing(ctx, repeaterID)
		if models.RepeaterIDExists(s.DB, repeaterID) {
			dbRepeater := models.FindRepeaterByID(s.DB, repeaterID)
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)
		} else {
			return
		}
		// Options is a string from data[8:]
		options := string(data[8:])
		if config.GetConfig().Debug {
			klog.Infof("Received Options from repeater %d: %s", repeaterID, options)
		}

		// https://github.com/g4klx/MMDVMHost/blob/master/DMRplus_startup_options.md
		// Options are not yet supported
	}
}

func (s *Server) handleRPTLPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTLPacket")
	defer span.End()

	// RPTL packets are 8 bytes long
	const rptlLen = 8
	const rptlRepeaterIDOffset = 4
	if len(data) != rptlLen {
		klog.Warningf("Invalid RPTL packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[rptlRepeaterIDOffset : rptlRepeaterIDOffset+repeaterIDLength]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	klog.Infof("Login from Repeater ID: %d", repeaterID)
	if !models.RepeaterIDExists(s.DB, repeaterID) {
		repeater := models.Repeater{}
		repeater.RadioID = repeaterID
		repeater.IP = remoteAddr.IP.String()
		repeater.Port = remoteAddr.Port
		repeater.Connection = "RPTL-RECEIVED"
		repeater.LastPing = time.Now()
		repeater.Connected = time.Now()
		s.Redis.storeRepeater(ctx, repeaterID, repeater)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
		if config.GetConfig().Debug {
			klog.Infof("Repeater ID %d is not valid, sending NAK", repeaterID)
		}
	} else {
		repeater := models.FindRepeaterByID(s.DB, repeaterID)
		bigSalt, err := rand.Int(rand.Reader, big.NewInt(max32Bit))
		if err != nil {
			klog.Exitf("Error generating random salt", err)
		}
		repeater.Salt = uint32(bigSalt.Uint64())
		repeater.IP = remoteAddr.IP.String()
		repeater.Port = remoteAddr.Port
		repeater.Connection = "RPTL-RECEIVED"
		repeater.LastPing = time.Now()
		repeater.Connected = time.Now()
		s.Redis.storeRepeater(ctx, repeaterID, repeater)
		// bigSalt.Bytes() can be less than 4 bytes, so we need make sure we prefix 0s
		var saltBytes [4]byte
		if len(bigSalt.Bytes()) < len(saltBytes) {
			copy(saltBytes[len(saltBytes)-len(bigSalt.Bytes()):], bigSalt.Bytes())
		} else {
			copy(saltBytes[:], bigSalt.Bytes())
		}
		s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, saltBytes[:])
		s.Redis.updateRepeaterConnection(ctx, repeaterID, "CHALLENGE_SENT")
	}
}

func (s *Server) handleRPTKPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTKPacket")
	defer span.End()

	// RPTK packets are 8 bytes long + a 32 byte sha256 hash
	const rptkLen = 40
	if len(data) != rptkLen {
		klog.Warningf("Invalid RPTK packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		klog.Infof("Challenge Response from Repeater ID: %d", repeaterID)
	}
	if s.validRepeater(ctx, repeaterID, "CHALLENGE_SENT", *remoteAddr) {
		password := ""
		var dbRepeater models.Repeater

		if models.RepeaterIDExists(s.DB, repeaterID) {
			dbRepeater = models.FindRepeaterByID(s.DB, repeaterID)
			password = dbRepeater.Password
		} else {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				klog.Infof("Repeater ID %d does not exist in db, sending NAK", repeaterID)
			}
			return
		}

		if password == "" {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				klog.Infof("Repeater ID %d did not provide password, sending NAK", repeaterID)
			}
			return
		}

		s.Redis.updateRepeaterPing(ctx, repeaterID)
		dbRepeater.LastPing = time.Now()
		s.DB.Save(&dbRepeater)

		repeater, err := s.Redis.getRepeater(ctx, repeaterID)
		if err != nil {
			klog.Errorf("Error getting repeater from redis: %v", err)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				klog.Infof("Repeater ID %d does not exist in redis, sending NAK", repeaterID)
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
			klog.Infof("Repeater ID %d authed, sending ACK", repeaterID)
			s.Redis.updateRepeaterConnection(ctx, repeaterID, "WAITING_CONFIG")
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

func (s *Server) handleRPTCLPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCLPacket")
	defer span.End()

	// RPTCL packets are 8 bytes long
	const rptclLen = 8
	if len(data) != rptclLen {
		klog.Warningf("Invalid RPTCL packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[5:9]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	klog.Infof("Disconnect from Repeater ID: %d", repeaterID)
	if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
	if !s.Redis.deleteRepeater(ctx, repeaterID) {
		klog.Warningf("Repeater ID %d not deleted", repeaterID)
	}
}

func (s *Server) handleRPTCPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTCPacket")
	defer span.End()

	// RPTC packets are 302 bytes long
	const rptcLen = 302
	if len(data) != rptcLen {
		klog.Warningf("Invalid RPTC packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[4:8]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	if config.GetConfig().Debug {
		klog.Infof("Repeater config from %d", repeaterID)
	}

	if s.validRepeater(ctx, repeaterID, "WAITING_CONFIG", *remoteAddr) {
		s.Redis.updateRepeaterPing(ctx, repeaterID)
		repeater, err := s.Redis.getRepeater(ctx, repeaterID)
		if err != nil {
			klog.Errorf("Error getting repeater from redis: %v", err)
			return
		}
		repeater.Connected = time.Now()
		repeater.LastPing = time.Now()

		repeater.Callsign = strings.ToUpper(strings.TrimRight(string(data[8:16]), " "))
		if len(repeater.Callsign) < 4 || len(repeater.Callsign) > 8 {
			klog.Errorf("Invalid callsign: %s", repeater.Callsign)
			return
		}
		if !dmrconst.CallsignRegex.MatchString(strings.ToUpper(repeater.Callsign)) {
			klog.Errorf("Invalid callsign: %s", repeater.Callsign)
			return
		}

		rxFreq, err := strconv.ParseInt(strings.TrimRight(string(data[16:25]), " "), 0, 32)
		if err != nil {
			klog.Errorf("Error parsing RXFreq", err)
			return
		}
		repeater.RXFrequency = uint(rxFreq)

		txFreq, err := strconv.ParseInt(strings.TrimRight(string(data[25:34]), " "), 0, 32)
		if err != nil {
			klog.Errorf("Error parsing TXFreq", err)
			return
		}
		repeater.TXFrequency = uint(txFreq)

		txPower, err := strconv.ParseInt(strings.TrimRight(string(data[34:36]), " "), 0, 32)
		if err != nil {
			klog.Errorf("Error parsing TXPower", err)
			return
		}
		repeater.TXPower = uint(txPower)
		const maxTXPower = 99
		if repeater.TXPower > maxTXPower {
			repeater.TXPower = maxTXPower
		}

		colorCode, err := strconv.ParseInt(strings.TrimRight(string(data[36:38]), " "), 0, 32)
		if err != nil {
			klog.Errorf("Error parsing ColorCode", err)
			return
		}
		const maxColorCode = 15
		if colorCode > maxColorCode {
			klog.Errorf("Invalid ColorCode: %d", colorCode)
			return
		}
		repeater.ColorCode = uint(colorCode)

		lat, err := strconv.ParseFloat(strings.TrimRight(string(data[38:46]), " "), 32)
		if err != nil {
			klog.Errorf("Error parsing Latitude", err)
			return
		}
		if lat < -90 || lat > 90 {
			klog.Errorf("Invalid Latitude: %f", lat)
			return
		}
		repeater.Latitude = float32(lat)

		long, err := strconv.ParseFloat(strings.TrimRight(string(data[46:55]), " "), 32)
		if err != nil {
			klog.Errorf("Error parsing Longitude", err)
			return
		}
		if long < -180 || long > 180 {
			klog.Errorf("Invalid Longitude: %f", long)
			return
		}
		repeater.Longitude = float32(long)

		height, err := strconv.ParseInt(strings.TrimRight(string(data[55:58]), " "), 0, 32)
		if err != nil {
			klog.Errorf("Error parsing Height", err)
			return
		}
		const maxHeight = 999
		if height > maxHeight {
			height = maxHeight
		}
		repeater.Height = int(height)

		repeater.Location = strings.TrimRight(string(data[58:78]), " ")
		const maxLocation = 20
		if len(repeater.Location) > maxLocation {
			repeater.Location = repeater.Location[:maxLocation]
		}

		repeater.Description = strings.TrimRight(string(data[78:97]), " ")
		const maxDescription = 20
		if len(repeater.Description) > maxDescription {
			repeater.Description = repeater.Description[:maxDescription]
		}

		slots, err := strconv.ParseInt(strings.TrimRight(string(data[97:98]), " "), 0, 32)
		if err != nil {
			klog.Errorf("Error parsing Slots", err)
			return
		}
		repeater.Slots = uint(slots)

		repeater.URL = strings.TrimRight(string(data[98:222]), " ")
		const maxURL = 124
		if len(repeater.URL) > maxURL {
			repeater.URL = repeater.URL[:maxURL]
		}

		repeater.SoftwareID = strings.TrimRight(string(data[222:262]), " ")
		const maxSoftwareID = 40
		if len(repeater.SoftwareID) > maxSoftwareID {
			repeater.SoftwareID = repeater.SoftwareID[:maxSoftwareID]
		} else if repeater.SoftwareID == "" {
			repeater.SoftwareID = "github.com/USA-RedDragon/DMRHub v" + sdk.Version + "-" + sdk.GitCommit
		}
		repeater.PackageID = strings.TrimRight(string(data[262:302]), " ")
		const maxPackageID = 40
		if len(repeater.PackageID) > maxPackageID {
			repeater.PackageID = repeater.PackageID[:maxPackageID]
		} else if repeater.PackageID == "" {
			repeater.PackageID = "v" + sdk.Version + "-" + sdk.GitCommit
		}

		repeater.Connection = "YES"
		s.Redis.storeRepeater(ctx, repeaterID, repeater)
		klog.Infof("Repeater ID %d (%s) connected\n", repeaterID, repeater.Callsign)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, repeaterIDBytes)
		dbRepeater := models.FindRepeaterByID(s.DB, repeaterID)
		dbRepeater.Connected = repeater.Connected
		dbRepeater.LastPing = repeater.LastPing
		dbRepeater.Callsign = repeater.Callsign
		dbRepeater.RXFrequency = repeater.RXFrequency
		dbRepeater.TXFrequency = repeater.TXFrequency
		dbRepeater.TXPower = repeater.TXPower
		dbRepeater.ColorCode = repeater.ColorCode
		dbRepeater.Latitude = repeater.Latitude
		dbRepeater.Longitude = repeater.Longitude
		dbRepeater.Height = repeater.Height
		dbRepeater.Location = repeater.Location
		dbRepeater.Description = repeater.Description
		dbRepeater.Slots = repeater.Slots
		dbRepeater.URL = repeater.URL
		dbRepeater.SoftwareID = repeater.SoftwareID
		dbRepeater.PackageID = repeater.PackageID
		s.DB.Save(&dbRepeater)
	} else {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
}

func (s *Server) handleRPTPINGPacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handleRPTPINGPacket")
	defer span.End()

	// RPTP packets are 11 bytes long
	const rptpLength = 11
	if len(data) != rptpLength {
		klog.Warningf("Invalid RPTP packet length: %d", len(data))
		return
	}
	repeaterIDBytes := data[7:11]
	repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
	klog.Infof("Ping from %d", repeaterID)

	if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
		s.Redis.updateRepeaterPing(ctx, repeaterID)
		dbRepeater := models.FindRepeaterByID(s.DB, repeaterID)
		if dbRepeater.RadioID == 0 {
			// No repeater found, drop
			klog.Warningf("No repeater found for ID %d", repeaterID)
			return
		}
		dbRepeater.LastPing = time.Now()
		s.DB.Save(&dbRepeater)
		repeater, err := s.Redis.getRepeater(ctx, repeaterID)
		if err != nil {
			klog.Errorf("Error getting repeater from Redis", err)
			return
		}
		repeater.PingsReceived++
		s.Redis.storeRepeater(ctx, repeaterID, repeater)
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTPONG, repeaterIDBytes)
	} else {
		s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
	}
}
