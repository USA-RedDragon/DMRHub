package dmr

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
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/USA-RedDragon/DMRHub/internal/sdk"
	"go.opentelemetry.io/otel"
	"k8s.io/klog/v2"
)

var tracer = otel.Tracer("dmr-server")

func (s *Server) validRepeater(ctx context.Context, repeaterID uint, connection string, remoteAddr net.UDPAddr) bool {
	valid := true
	if !s.Redis.exists(ctx, repeaterID) {
		klog.Warningf("Repeater %d does not exist", repeaterID)
		valid = false
	}
	repeater, err := s.Redis.get(ctx, repeaterID)
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
				go repeater.ListenForCallsOn(ctx, s.Redis.Redis, packet.Dst)
				s.DB.Save(&repeater)
			}
		} else {
			if repeater.TS1DynamicTalkgroupID == nil || *repeater.TS1DynamicTalkgroupID != packet.Dst {
				klog.Infof("Dynamically Linking %d timeslot 1 to %d", packet.Repeater, packet.Dst)
				repeater.TS1DynamicTalkgroup = talkgroup
				repeater.TS1DynamicTalkgroupID = &packet.Dst
				go repeater.ListenForCallsOn(ctx, s.Redis.Redis, packet.Dst)
				s.DB.Save(&repeater)
			}
		}
	} else if config.GetConfig().Debug {
		klog.Infof("Repeater %d not found in DB", packet.Repeater)
	}
}

func (s *Server) handlePacket(remoteAddr *net.UDPAddr, data []byte) {
	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "handlePacket")
	defer span.End()
	if len(data) < 4 {
		// Not enough data here to be a valid packet
		klog.Warningf("Invalid packet length: %d", len(data))
		return
	}
	// Extract the command, which is various length, all but one 4 significant characters -- RPTCL
	command := dmrconst.Command(data[:4])
	if command == dmrconst.CommandDMRA {
		if len(data) < 15 {
			klog.Warningf("Invalid packet length: %d", len(data))
			return
		}

		repeaterIDBytes := data[4:8]
		repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
		if config.GetConfig().Debug {
			klog.Infof("DMR talk alias from Repeater ID: %d", repeaterIDBytes)
		}
		if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
			s.Redis.ping(ctx, repeaterID)
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
	} else if command == dmrconst.CommandDMRD {
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
			s.Redis.ping(ctx, repeaterID)

			var dbRepeater models.Repeater
			if models.RepeaterIDExists(s.DB, repeaterID) {
				dbRepeater = models.FindRepeaterByID(s.DB, repeaterID)
				dbRepeater.LastPing = time.Now()
				s.DB.Save(&dbRepeater)
			} else {
				klog.Warningf("Repeater %d not found in DB", repeaterID)
				return
			}
			packet := models.UnpackPacket(data[:])

			if config.GetConfig().Debug {
				klog.Infof("DMRD packet: %s", packet.String())
			}

			isVoice := false
			isData := false
			switch packet.FrameType {
			case dmrconst.FrameDataSync:
				if dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
					isVoice = true
					if config.GetConfig().Debug {
						klog.Infof("Voice terminator from %d", packet.Src)
					}
				} else if dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceHead {
					isVoice = true
					if config.GetConfig().Debug {
						klog.Infof("Voice header from %d", packet.Src)
					}
				} else {
					isData = true
					if config.GetConfig().Debug {
						klog.Infof("Data packet from %d, dtype: %d", packet.Src, packet.DTypeOrVSeq)
					}
				}
			case dmrconst.FrameVoice:
				isVoice = true
				if config.GetConfig().Debug {
					klog.Infof("Voice packet from %d, vseq %d", packet.Src, packet.DTypeOrVSeq)
				}
			case dmrconst.FrameVoiceSync:
				isVoice = true
				if config.GetConfig().Debug {
					klog.Infof("Voice sync packet from %d, dtype: %d", packet.Src, packet.DTypeOrVSeq)
				}
			}

			if packet.Dst == 0 {
				return
			}

			// Don't call track unlink
			if packet.Dst != 4000 && isVoice {
				go func() {
					if !s.CallTracker.IsCallActive(packet) {
						s.CallTracker.StartCall(ctx, packet)
					}
					if s.CallTracker.IsCallActive(packet) {
						s.CallTracker.ProcessCallPacket(ctx, packet)
						if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
							s.CallTracker.EndCall(ctx, packet)
						}
					}
				}()
			}

			if packet.Dst == 9990 && isVoice {
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
						time.Sleep(3 * time.Second)
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
							// If elapsed is greater than 60ms, we're behind and need to catch up
							if elapsed > 60*time.Millisecond {
								klog.Warningf("Parrot call took too long to send, elapsed: %s", elapsed)
								// Sleep for 60ms minus the difference between the elapsed time and 60ms
								time.Sleep(60*time.Millisecond - (elapsed - 60*time.Millisecond))
							} else {
								// Now subtract the elapsed time from 60ms to get the true delay
								delay := 60*time.Millisecond - elapsed
								time.Sleep(delay)
							}
							startedTime = time.Now()
						}
					}()
				}
				// Don't route parrot calls
				return
			}

			if packet.Dst == 4000 && isVoice {
				if packet.Slot {
					klog.Infof("Unlinking timeslot 2 from %d", packet.Repeater)
					if dbRepeater.TS2DynamicTalkgroupID != nil {
						oldTGID := *dbRepeater.TS2DynamicTalkgroupID
						s.DB.Model(&dbRepeater).Select("TS2DynamicTalkgroupID").Updates(map[string]interface{}{"TS2DynamicTalkgroupID": nil})
						err := s.DB.Model(&dbRepeater).Association("TS2DynamicTalkgroup").Delete(&dbRepeater.TS2DynamicTalkgroup)
						if err != nil {
							klog.Errorf("Error deleting TS2DynamicTalkgroup: %s", err)
						}
						dbRepeater.CancelSubscription(oldTGID)
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
						dbRepeater.CancelSubscription(oldTGID)
					}
				}
				s.DB.Save(&dbRepeater)
				return
			}

			if packet.GroupCall && isVoice {
				go s.switchDynamicTalkgroup(ctx, packet)

				// We can just use redis to publish to "packets:talkgroup:<id>"
				var rawPacket models.RawDMRPacket
				rawPacket.Data = data[:]
				rawPacket.RemoteIP = remoteAddr.IP.String()
				rawPacket.RemotePort = remoteAddr.Port
				packedBytes, err := rawPacket.MarshalMsg(nil)
				if err != nil {
					klog.Errorf("Error marshalling raw packet", err)
					return
				}
				s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:talkgroup:%d", packet.Dst), packedBytes)
			} else if !packet.GroupCall && isVoice {
				// packet.Dst is either a repeater or a user
				// If it's a repeater, we need to send it to the repeater
				// If it's a user, we need to send it to the repeater that the user is connected to
				// by looking up the user in the database and iterating through their repeaters

				var rawPacket models.RawDMRPacket
				rawPacket.Data = data[:]
				rawPacket.RemoteIP = remoteAddr.IP.String()
				rawPacket.RemotePort = remoteAddr.Port

				packedBytes, err := rawPacket.MarshalMsg(nil)
				if err != nil {
					klog.Errorf("Error marshalling raw packet", err)
					return
				}

				// users have 7 digit IDs, repeaters have 6 digit IDs or 9 digit IDs
				if packet.Dst < 1000000 || packet.Dst > 99999999 {
					// This is to a repeater
					s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:repeater:%d", packet.Dst), packedBytes)
				} else if packet.Dst < 10000000 {
					// This is to a user
					// Search the database for the user
					if models.UserIDExists(s.DB, packet.Dst) {
						user := models.FindUserByID(s.DB, packet.Dst)
						// Query lastheard where UserID == user.ID LIMIT 1
						var lastCall models.Call
						s.DB.Where("user_id = ?", user.ID).Order("created_at DESC").First(&lastCall)
						if s.DB.Error != nil {
							klog.Errorf("Error querying last call for user %d: %s", user.ID, s.DB.Error)
						} else {
							// If the last call exists that that repeater is online
							if lastCall.ID != 0 && s.Redis.exists(ctx, lastCall.RepeaterID) {
								// Send the packet to the last user call's repeater
								s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:repeater:%d", lastCall.RepeaterID), packedBytes)
							}
						}

						// For each user repeaters
						for _, repeater := range user.Repeaters {
							// If the repeater is online and the last user call was not to this repeater
							if repeater.RadioID != lastCall.RepeaterID && s.Redis.exists(ctx, repeater.RadioID) {
								// Send the packet to the repeater
								s.Redis.Redis.Publish(ctx, fmt.Sprintf("packets:repeater:%d", repeater.RadioID), packedBytes)
							}
						}
					}
				}
			} else if isData {
				klog.Warning("Unhandled data packet type")
			} else {
				klog.Warning("Unhandled packet type")
			}
		}
	} else if command == dmrconst.CommandRPTO {
		if len(data) < 8 {
			klog.Warning("RPTO packet too short")
			return
		}
		if len(data) > 300 {
			klog.Warning("RPTO packet too long")
			return
		}

		repeaterIDBytes := data[4:8]
		repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
		if config.GetConfig().Debug {
			klog.Infof("Set options from %d", repeaterID)
		}

		if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
			s.Redis.ping(ctx, repeaterID)
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
	} else if command == dmrconst.CommandRPTL {
		// RPTL packets are 8 bytes long
		if len(data) != 8 {
			klog.Warningf("Invalid RPTL packet length: %d", len(data))
			return
		}
		repeaterIDBytes := data[4:8]
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
			s.Redis.store(ctx, repeaterID, repeater)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			if config.GetConfig().Debug {
				klog.Infof("Repeater ID %d is not valid, sending NAK", repeaterID)
			}
		} else {
			repeater := models.FindRepeaterByID(s.DB, repeaterID)
			bigSalt, err := rand.Int(rand.Reader, big.NewInt(0xFFFFFFFF))
			if err != nil {
				klog.Exitf("Error generating random salt", err)
			}
			repeater.Salt = uint32(bigSalt.Uint64())
			repeater.IP = remoteAddr.IP.String()
			repeater.Port = remoteAddr.Port
			repeater.Connection = "RPTL-RECEIVED"
			repeater.LastPing = time.Now()
			repeater.Connected = time.Now()
			s.Redis.store(ctx, repeaterID, repeater)
			// bigSalt.Bytes() can be less than 4 bytes, so we need make sure we prefix 0s
			var saltBytes [4]byte
			if len(bigSalt.Bytes()) < 4 {
				copy(saltBytes[4-len(bigSalt.Bytes()):], bigSalt.Bytes())
			} else {
				copy(saltBytes[:], bigSalt.Bytes())
			}
			s.sendCommand(ctx, repeaterID, dmrconst.CommandRPTACK, saltBytes[:])
			s.Redis.updateConnection(ctx, repeaterID, "CHALLENGE_SENT")
		}
	} else if command == dmrconst.CommandRPTK {
		// RPTL packets are 8 bytes long + a 32 byte sha256 hash
		if len(data) != 40 {
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

			s.Redis.ping(ctx, repeaterID)
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)

			repeater, err := s.Redis.get(ctx, repeaterID)
			if err != nil {
				klog.Errorf("Error getting repeater from redis: %v", err)
				s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
				if config.GetConfig().Debug {
					klog.Infof("Repeater ID %d does not exist in redis, sending NAK", repeaterID)
				}
			}
			rxSalt := binary.BigEndian.Uint32(data[8:])
			// sha256 hash repeater.Salt + the passphrase
			saltBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(saltBytes, repeater.Salt)
			hash := sha256.Sum256(append(saltBytes, []byte(password)...))
			calcedSalt := binary.BigEndian.Uint32(hash[:])
			if calcedSalt == rxSalt {
				klog.Infof("Repeater ID %d authed, sending ACK", repeaterID)
				s.Redis.updateConnection(ctx, repeaterID, "WAITING_CONFIG")
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
	} else if command == dmrconst.CommandRPTC {
		if dmrconst.Command(data[:5]) == dmrconst.CommandRPTCL {
			// RPTCL packets are 8 bytes long
			if len(data) != 8 {
				klog.Warningf("Invalid RPTCL packet length: %d", len(data))
				return
			}
			repeaterIDBytes := data[5:9]
			repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
			klog.Infof("Disconnect from Repeater ID: %d", repeaterID)
			if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
				s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
			}
			if !s.Redis.delete(ctx, repeaterID) {
				klog.Warningf("Repeater ID %d not deleted", repeaterID)
			}
		} else {
			// RPTC packets are 302 bytes long
			if len(data) != 302 {
				klog.Warningf("Invalid RPTC packet length: %d", len(data))
				return
			}
			repeaterIDBytes := data[4:8]
			repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
			if config.GetConfig().Debug {
				klog.Infof("Repeater config from %d", repeaterID)
			}

			if s.validRepeater(ctx, repeaterID, "WAITING_CONFIG", *remoteAddr) {
				s.Redis.ping(ctx, repeaterID)
				repeater, err := s.Redis.get(ctx, repeaterID)
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
				if repeater.TXPower > 99 {
					repeater.TXPower = 99
				}

				colorCode, err := strconv.ParseInt(strings.TrimRight(string(data[36:38]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing ColorCode", err)
					return
				}
				if colorCode > 15 {
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
				if height > 999 {
					height = 999
				}
				repeater.Height = int(height)

				repeater.Location = strings.TrimRight(string(data[58:78]), " ")
				if len(repeater.Location) > 20 {
					repeater.Location = repeater.Location[:20]
				}

				repeater.Description = strings.TrimRight(string(data[78:97]), " ")
				if len(repeater.Description) > 20 {
					repeater.Description = repeater.Description[:20]
				}

				slots, err := strconv.ParseInt(strings.TrimRight(string(data[97:98]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing Slots", err)
					return
				}
				repeater.Slots = uint(slots)

				repeater.URL = strings.TrimRight(string(data[98:222]), " ")
				if len(repeater.URL) > 124 {
					repeater.URL = repeater.URL[:124]
				}

				repeater.SoftwareID = strings.TrimRight(string(data[222:262]), " ")
				if len(repeater.SoftwareID) > 40 {
					repeater.SoftwareID = repeater.SoftwareID[:40]
				} else if repeater.SoftwareID == "" {
					repeater.SoftwareID = "github.com/USA-RedDragon/DMRHub v" + sdk.Version + "-" + sdk.GitCommit
				}
				repeater.PackageID = strings.TrimRight(string(data[262:302]), " ")
				if len(repeater.PackageID) > 40 {
					repeater.PackageID = repeater.PackageID[:40]
				} else if repeater.PackageID == "" {
					repeater.PackageID = "v" + sdk.Version + "-" + sdk.GitCommit
				}

				repeater.Connection = "YES"
				s.Redis.store(ctx, repeaterID, repeater)
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
	} else if command == dmrconst.CommandRPTPING[:4] {
		// RPTP packets are 11 bytes long
		if len(data) != 11 {
			klog.Warningf("Invalid RPTP packet length: %d", len(data))
			return
		}
		repeaterIDBytes := data[7:11]
		repeaterID := uint(binary.BigEndian.Uint32(repeaterIDBytes))
		klog.Infof("Ping from %d", repeaterID)

		if s.validRepeater(ctx, repeaterID, "YES", *remoteAddr) {
			s.Redis.ping(ctx, repeaterID)
			dbRepeater := models.FindRepeaterByID(s.DB, repeaterID)
			if dbRepeater.RadioID == 0 {
				// No repeater found, drop
				klog.Warningf("No repeater found for ID %d", repeaterID)
				return
			}
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)
			repeater, err := s.Redis.get(ctx, repeaterID)
			if err != nil {
				klog.Errorf("Error getting repeater from Redis", err)
				return
			}
			repeater.PingsReceived++
			s.Redis.store(ctx, repeaterID, repeater)
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTPONG, repeaterIDBytes)
		} else {
			s.sendCommand(ctx, repeaterID, dmrconst.CommandMSTNAK, repeaterIDBytes)
		}
	} else if command == dmrconst.CommandRPTACK[:4] {
		klog.Warning("TODO: RPTACK")
		// I don't think we ever receive this
	} else if command == dmrconst.CommandMSTCL[:4] {
		klog.Warning("TODO: MSTCL")
		// I don't think we ever receive this
	} else if command == dmrconst.CommandMSTNAK[:4] {
		klog.Warning("TODO: MSTNAK")
		// I don't think we ever receive this
	} else if command == dmrconst.CommandMSTPONG[:4] {
		klog.Warning("TODO: MSTPONG")
		// I don't think we ever receive this
	} else if command == dmrconst.CommandRPTSBKN[:4] {
		klog.Warning("TODO: RPTSBKN")
		// I don't think we ever receive this
	} else {
		klog.Warning("Unknown Command: %s", command)
	}
}
