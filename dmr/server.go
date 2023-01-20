package dmr

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type DMRServer struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Started       bool
	Parrot        *Parrot
	Verbose       bool
	DB            *gorm.DB
	Redis         redisRepeaterStorage
	CallTracker   *CallTracker
}

func MakeServer(addr string, port int, redisHost string, verbose bool, db *gorm.DB) DMRServer {
	return DMRServer{
		Buffer: make([]byte, 302),
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(addr),
			Port: port,
		},
		Started:     false,
		Parrot:      NewParrot(redisHost),
		Verbose:     verbose,
		DB:          db,
		Redis:       makeRedisRepeaterStorage(redisHost),
		CallTracker: NewCallTracker(db),
	}
}

func (s DMRServer) Stop() {
	// Send a MSTCL command to each repeater
	repeaters, err := s.Redis.list()
	if err != nil {
		klog.Errorf("Error scanning redis for repeaters", err)
	}
	for _, repeater := range repeaters {
		if s.Verbose {
			klog.Infof("Repeater found: %d", repeater)
		}
		s.Redis.updateConnection(repeater, "DISCONNECTED")
		repeaterBinary := make([]byte, 4)
		binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
		s.sendCommand(repeater, COMMAND_MSTCL, repeaterBinary)
	}
	s.Started = false
}

func (s DMRServer) validRepeater(repeaterID uint, connection string, remoteAddr net.UDPAddr) bool {
	valid := true
	if !s.Redis.exists(repeaterID) {
		klog.Warningf("Repeater %d does not exist", repeaterID)
		valid = false
	}
	repeater, err := s.Redis.get(repeaterID)
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

func (s DMRServer) Listen() {
	klog.Infof("DMR Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	// 1MB buffers, say what?
	server.SetReadBuffer(1000000)
	server.SetWriteBuffer(1000000)
	s.Server = server
	s.Started = true
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}

	for {
		len, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
		if s.Verbose {
			klog.Infof("Read a message from %v\n", remoteaddr)
		}
		if err != nil {
			klog.Warningf("Error reading from UDP Socket, Swallowing Error: %v", err)
			continue
		}
		s.handlePacket(remoteaddr, s.Buffer[:len])
	}
}

func (s DMRServer) switchDynamicTalkgroup(packet models.Packet) {
	// If the source repeater's (`packet.Repeater`) database entry's
	// `TS1DynamicTalkgroupID` or `TS2DynamicTalkgroupID` (respective
	// of the current `packet.Slot`) doesn't match the packet's `Dst`
	// field, then we need to update the database entry to reflect
	// the new dynamic talkgroup on the appropriate slot.
	if models.RepeaterIDExists(s.DB, packet.Repeater) {
		if !models.TalkgroupIDExists(s.DB, packet.Dst) {
			if s.Verbose {
				klog.Infof("Repeater %d not found in DB", packet.Repeater)
			}
			return
		}
		repeater := models.FindRepeaterByID(s.DB, packet.Repeater)
		talkgroup := models.FindTalkgroupByID(s.DB, packet.Dst)
		if packet.Slot {
			if repeater.TS2DynamicTalkgroupID != packet.Dst {
				klog.Infof("Dynamically Linking %d timeslot 2 to %d", packet.Repeater, packet.Dst)
				repeater.TS2DynamicTalkgroup = talkgroup
				repeater.TS2DynamicTalkgroupID = packet.Dst
				s.DB.Save(&repeater)
			}
		} else {
			if repeater.TS1DynamicTalkgroupID != packet.Dst {
				klog.Infof("Dynamically Linking %d timeslot 1 to %d", packet.Repeater, packet.Dst)
				repeater.TS1DynamicTalkgroup = talkgroup
				repeater.TS1DynamicTalkgroupID = packet.Dst
				s.DB.Save(&repeater)
			}
		}
	} else if s.Verbose {
		klog.Infof("Repeater %d not found in DB", packet.Repeater)
	}
}

func (s DMRServer) sendCommand(repeaterIdBytes uint, command string, data []byte) {
	if !s.Started {
		klog.Warningf("Server not started, not sending command")
		return
	}
	if s.Verbose {
		klog.Infof("Sending Command %s to Repeater ID: %d", command, repeaterIdBytes)
	}
	command_prefixed_data := append([]byte(command), data...)
	repeater, err := s.Redis.get(repeaterIdBytes)
	if err != nil {
		klog.Errorf("Error getting repeater from Redis", err)
		return
	}
	_, err = s.Server.WriteToUDP(command_prefixed_data, &net.UDPAddr{
		IP:   net.ParseIP(repeater.IP),
		Port: repeater.Port,
	})
	if err != nil {
		klog.Errorf("Error writing to UDP Socket", err)
	}
}

func (s DMRServer) sendPacket(repeaterIdBytes uint, packet models.Packet) {
	if s.Verbose {
		klog.Infof("Sending Packet: %v\n", packet)
		klog.Infof("Sending DMR packet to Repeater ID: %d", repeaterIdBytes)
	}
	repeater, err := s.Redis.get(repeaterIdBytes)
	if err != nil {
		klog.Errorf("Error getting repeater from Redis", err)
		return
	}
	_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
		IP:   net.ParseIP(repeater.IP),
		Port: repeater.Port,
	})
	if err != nil {
		klog.Errorf("Error writing to UDP Socket", err)
	}
}

func (s DMRServer) handlePacket(remoteAddr *net.UDPAddr, data []byte) {
	if s.Verbose {
		klog.Infof("Data: %s", string(data[:]))
	}
	// Extract the command, which is various length, all but one 4 significant characters -- RPTCL
	command := string(data[:4])
	if command == COMMAND_DMRA {
		repeaterIdBytes := data[4:8]
		repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
		if s.Verbose {
			klog.Infof("DMR talk alias from Repeater ID: %d", repeaterIdBytes)
		}
		if s.validRepeater(repeaterId, "YES", *remoteAddr) {
			s.Redis.ping(repeaterId)
			dbRepeater := models.FindRepeaterByID(s.DB, repeaterId)
			if dbRepeater.RadioID == 0 {
				// Repeater not found, drop
				klog.Warningf("Repeater %d not found in DB", repeaterId)
				return
			}
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)
			klog.Warning("TODO: DMRA")
		}
	} else if command == COMMAND_DMRD {
		repeaterIdBytes := data[11:15]
		repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
		if s.Verbose {
			klog.Infof("DMR Data from Repeater ID: %d", repeaterId)
		}
		if s.validRepeater(repeaterId, "YES", *remoteAddr) {
			s.Redis.ping(repeaterId)

			var dbRepeater models.Repeater
			if models.RepeaterIDExists(s.DB, repeaterId) {
				dbRepeater = models.FindRepeaterByID(s.DB, repeaterId)
				dbRepeater.LastPing = time.Now()
				s.DB.Save(&dbRepeater)
			} else {
				klog.Warningf("Repeater %d not found in DB", repeaterId)
				return
			}
			// If the packet length is 11 exactly, it's a packet header without any data
			// we may drop it
			if len(data) == 11 {
				klog.Warningf("Packet header without data, dropping")
				return
			}
			packet := models.UnpackPacket(data[:])

			// If packet.BER is not 0, print it
			if packet.BER != -1 {
				klog.Infof("BER: %d", packet.BER)
			}

			// If packet.RSSI is not 0, print it
			if packet.RSSI != -1 {
				klog.Infof("RSSI: %ddBm", packet.RSSI)
			}

			isVoice := false
			isData := false
			switch packet.FrameType {
			case HBPF_DATA_SYNC:
				if packet.DTypeOrVSeq == HBPF_SLT_VTERM {
					isVoice = true
					klog.Infof("Voice terminator from %d", packet.Src)
				} else if packet.DTypeOrVSeq == HBPF_SLT_VHEAD {
					isVoice = true
					klog.Infof("Voice header from %d", packet.Src)
				} else {
					isData = true
					klog.Infof("Data packet from %d, dtype: %d", packet.Src, packet.DTypeOrVSeq)
				}
			case HBPF_VOICE:
				isVoice = true
				klog.Infof("Voice packet from %d, vseq %d", packet.Src, packet.DTypeOrVSeq)
			case HBPF_VOICE_SYNC:
				isVoice = true
				klog.Infof("Voice sync packet from %d, dtype: %d", packet.Src, packet.DTypeOrVSeq)
			}

			if packet.Dst == 0 {
				return
			}

			// Don't call track unlink
			if packet.Dst != 4000 && isVoice {
				if !s.CallTracker.IsCallActive(packet) {
					s.CallTracker.StartCall(packet)
				}
				s.CallTracker.ProcessCallPacket(packet)
				if packet.FrameType == HBPF_DATA_SYNC && packet.DTypeOrVSeq == HBPF_SLT_VTERM {
					s.CallTracker.EndCall(packet)
				}
			}

			if packet.Dst == 9990 && isVoice {
				klog.Infof("Parrot call from %d", packet.Src)
				if !s.Parrot.IsStarted(packet.StreamId) {
					s.Parrot.StartStream(packet.StreamId, repeaterId)
				}
				s.Parrot.RecordPacket(packet.StreamId, packet)
				if packet.FrameType == HBPF_DATA_SYNC && packet.DTypeOrVSeq == HBPF_SLT_VTERM {
					s.Parrot.StopStream(packet.StreamId)
					f := func() {
						packets := s.Parrot.GetStream(packet.StreamId)
						time.Sleep(3 * time.Second)
						started := false
						// Track the duration of the call to ensure that we send out packets right on the 60ms boundary
						// This is to ensure that the DMR repeater doesn't drop the packet
						startedTime := time.Now()
						for j, pkt := range packets {
							s.sendPacket(repeaterId, pkt)
							if !started {
								s.CallTracker.StartCall(pkt)
								started = true
							}
							pkt.RSSI = -1
							pkt.BER = -1
							s.CallTracker.ProcessCallPacket(pkt)
							if j == len(packets)-1 {
								s.CallTracker.EndCall(pkt)
							}
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
					}
					go f()
				}
				// Don't route parrot calls
				return
			}

			if packet.Dst == 4000 && isVoice {
				if packet.Slot {
					klog.Infof("Unlinking timeslot 2 from %d", packet.Repeater)
					s.DB.Model(&dbRepeater).Select("TS2DynamicTalkgroupID").Updates(map[string]interface{}{"TS2DynamicTalkgroupID": nil})
					s.DB.Model(&dbRepeater).Association("TS2DynamicTalkgroup").Delete(&dbRepeater.TS2DynamicTalkgroup)
				} else {
					klog.Infof("Unlinking timeslot 1 from %d", packet.Repeater)
					s.DB.Model(&dbRepeater).Select("TS1DynamicTalkgroupID").Updates(map[string]interface{}{"TS1DynamicTalkgroupID": nil})
					s.DB.Model(&dbRepeater).Association("TS1DynamicTalkgroup").Delete(&dbRepeater.TS1DynamicTalkgroup)
				}
				s.DB.Save(&dbRepeater)
				return
			}

			if packet.GroupCall && isVoice {
				go s.switchDynamicTalkgroup(packet)

				// For each repeater in Redis
				repeaters, err := s.Redis.list()
				if err != nil {
					klog.Errorf("Error scanning redis for repeaters", err)
				}
				for _, repeater := range repeaters {
					if repeater == repeaterId {
						continue
					} else {
						var destDbRepeater models.Repeater
						if !models.RepeaterIDExists(s.DB, repeater) {
							klog.Warningf("Repeater %d not found in DB", repeater)
							return
						}
						destDbRepeater = models.FindRepeaterByID(s.DB, repeater)
						klog.Infof("Checking if repeater %d wants packets", repeater)
						want, slot := destDbRepeater.WantRX(packet)
						if want {
							klog.Infof("Repeater %d wants packet", destDbRepeater.RadioID)
							packet.Slot = slot
							if s.Verbose {
								slotNum := 1
								if packet.Slot {
									slotNum = 2
								}
								klog.Infof("Sending packet to repeater %d on slot %d", destDbRepeater.RadioID, slotNum)
							}
							packet.Repeater = destDbRepeater.RadioID
							go s.sendPacket(destDbRepeater.RadioID, packet)
						} else {
							klog.Infof("Repeater %d does not want packet", dbRepeater.RadioID)
						}
					}
				}
			} else if !packet.GroupCall && isVoice {
				if s.Redis.exists(packet.Dst) {
					packet.Repeater = packet.Dst
					s.sendPacket(repeaterId, packet)
				} else {
					klog.Warning("Private call to non-existent repeater %d", packet.Dst)
				}
			} else if isData {
				klog.Warning("Unhandled data packet type")
			} else {
				klog.Warning("Unhandled packet type")
			}
		}
	} else if command == COMMAND_RPTO {
		repeaterIdBytes := data[4:8]
		repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
		if s.Verbose {
			klog.Infof("Set options from %d", repeaterId)
		}

		if s.validRepeater(repeaterId, "YES", *remoteAddr) {
			s.Redis.ping(repeaterId)
			if models.RepeaterIDExists(s.DB, repeaterId) {
				dbRepeater := models.FindRepeaterByID(s.DB, repeaterId)
				dbRepeater.LastPing = time.Now()
				s.DB.Save(&dbRepeater)
			} else {
				return
			}
			klog.Warning("TODO: RPTO")
			s.sendCommand(repeaterId, COMMAND_RPTACK, repeaterIdBytes)
		}
	} else if command == COMMAND_RPTL {
		repeaterIdBytes := data[4:8]
		repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
		klog.Infof("Login from Repeater ID: %d", repeaterId)
		if !models.RepeaterIDExists(s.DB, repeaterId) {
			repeater := models.Repeater{}
			repeater.RadioID = repeaterId
			repeater.IP = remoteAddr.IP.String()
			repeater.Port = remoteAddr.Port
			repeater.Connection = "RPTL-RECEIVED"
			repeater.LastPing = time.Now()
			repeater.Connected = time.Now()
			s.Redis.store(repeaterId, repeater)
			s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
			if s.Verbose {
				klog.Infof("Repeater ID %d is not valid, sending NAK", repeaterId)
			}
		} else {
			repeater := models.FindRepeaterByID(s.DB, repeaterId)
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
			s.Redis.store(repeaterId, repeater)
			// bigSalt.Bytes() can be less than 4 bytes, so we need make sure we prefix 0s
			var saltBytes [4]byte
			if len(bigSalt.Bytes()) < 4 {
				copy(saltBytes[4-len(bigSalt.Bytes()):], bigSalt.Bytes())
			} else {
				copy(saltBytes[:], bigSalt.Bytes())
			}
			s.sendCommand(repeaterId, COMMAND_RPTACK, saltBytes[:])
			s.Redis.updateConnection(repeaterId, "CHALLENGE_SENT")
		}
	} else if command == COMMAND_RPTK {
		repeaterIdBytes := data[4:8]
		repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
		if s.Verbose {
			klog.Infof("Challenge Response from Repeater ID: %d", repeaterId)
		}
		if s.validRepeater(repeaterId, "CHALLENGE_SENT", *remoteAddr) {
			password := ""
			var dbRepeater models.Repeater

			if models.RepeaterIDExists(s.DB, repeaterId) {
				dbRepeater = models.FindRepeaterByID(s.DB, repeaterId)
				password = dbRepeater.Password
			} else {
				s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
				if s.Verbose {
					klog.Infof("Repeater ID %d does not exist in db, sending NAK", repeaterId)
				}
				return
			}

			if password == "" {
				s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
				if s.Verbose {
					klog.Infof("Repeater ID %d did not provide password, sending NAK", repeaterId)
				}
				return
			}

			s.Redis.ping(repeaterId)
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)

			repeater, err := s.Redis.get(repeaterId)
			if err != nil {
				klog.Errorf("Error getting repeater from redis: %v", err)
				s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
				if s.Verbose {
					klog.Infof("Repeater ID %d does not exist in redis, sending NAK", repeaterId)
				}
			}
			rxSalt := binary.BigEndian.Uint32(data[8:])
			// sha256 hash repeater.Salt + the passphrase
			saltBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(saltBytes, repeater.Salt)
			hash := sha256.Sum256(append(saltBytes, []byte(password)...))
			calcedSalt := binary.BigEndian.Uint32(hash[:])
			if calcedSalt == rxSalt {
				klog.Infof("Repeater ID %d authed, sending ACK", repeaterId)
				s.Redis.updateConnection(repeaterId, "WAITING_CONFIG")
				s.sendCommand(repeaterId, COMMAND_RPTACK, repeaterIdBytes)
			} else {
				s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
			}
		} else {
			s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
		}
	} else if command == COMMAND_RPTC {
		if string(data[:5]) == COMMAND_RPTCL {
			repeaterIdBytes := data[5:9]
			repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
			klog.Infof("Disconnect from Repeater ID: %d", repeaterId)
			if s.validRepeater(repeaterId, "YES", *remoteAddr) {
				s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
			}
			if !s.Redis.delete(repeaterId) {
				klog.Warningf("Repeater ID %d not deleted", repeaterId)
			}
		} else {
			repeaterIdBytes := data[4:8]
			repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
			if s.Verbose {
				klog.Infof("Repeater config from %d", repeaterId)
			}

			if s.validRepeater(repeaterId, "WAITING_CONFIG", *remoteAddr) {
				s.Redis.ping(repeaterId)
				repeater, err := s.Redis.get(repeaterId)
				if err != nil {
					klog.Errorf("Error getting repeater from redis: %v", err)
					return
				}
				repeater.Connected = time.Now()
				repeater.LastPing = time.Now()

				repeater.Callsign = strings.TrimRight(string(data[8:16]), " ")
				rxFreq, err := strconv.ParseInt(strings.TrimRight(string(data[16:25]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing RXFreq", err)
					return
				}
				repeater.RXFrequency = int(rxFreq)
				txFreq, err := strconv.ParseInt(strings.TrimRight(string(data[25:34]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing TXFreq", err)
					return
				}
				repeater.TXFrequency = int(txFreq)
				txPower, err := strconv.ParseInt(strings.TrimRight(string(data[34:36]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing TXPower", err)
					return
				}
				repeater.TXPower = int(txPower)
				colorCode, err := strconv.ParseInt(strings.TrimRight(string(data[36:38]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing ColorCode", err)
					return
				}
				repeater.ColorCode = uint(colorCode)
				lat, err := strconv.ParseFloat(strings.TrimRight(string(data[38:46]), " "), 32)
				if err != nil {
					klog.Errorf("Error parsing Latitude", err)
					return
				}
				repeater.Latitude = float32(lat)
				long, err := strconv.ParseFloat(strings.TrimRight(string(data[46:55]), " "), 32)
				if err != nil {
					klog.Errorf("Error parsing Longitude", err)
					return
				}
				repeater.Longitude = float32(long)
				height, err := strconv.ParseInt(strings.TrimRight(string(data[55:58]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing Height", err)
					return
				}
				repeater.Height = int(height)
				repeater.Location = strings.TrimRight(string(data[58:78]), " ")
				repeater.Description = strings.TrimRight(string(data[78:97]), " ")
				slots, err := strconv.ParseInt(strings.TrimRight(string(data[97:98]), " "), 0, 32)
				if err != nil {
					klog.Errorf("Error parsing Slots", err)
					return
				}
				repeater.Slots = int(slots)
				repeater.URL = strings.TrimRight(string(data[98:222]), " ")
				repeater.SoftwareID = strings.TrimRight(string(data[222:262]), " ")
				repeater.PackageID = strings.TrimRight(string(data[262:302]), " ")
				repeater.Connection = "YES"
				s.Redis.store(repeaterId, repeater)
				klog.Infof("Repeater ID %d (%s) connected\n", repeaterId, repeater.Callsign)
				s.sendCommand(repeaterId, COMMAND_RPTACK, repeaterIdBytes)
				dbRepeater := models.FindRepeaterByID(s.DB, repeaterId)
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
				s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
			}
		}
	} else if command == COMMAND_RPTP {
		repeaterIdBytes := data[7:11]
		repeaterId := uint(binary.BigEndian.Uint32(repeaterIdBytes))
		klog.Infof("Ping from %d", repeaterId)

		if s.validRepeater(repeaterId, "YES", *remoteAddr) {
			s.Redis.ping(repeaterId)
			dbRepeater := models.FindRepeaterByID(s.DB, repeaterId)
			if dbRepeater.RadioID == 0 {
				// No repeater found, drop
				klog.Warningf("No repeater found for ID %d", repeaterId)
				return
			}
			dbRepeater.LastPing = time.Now()
			s.DB.Save(&dbRepeater)
			repeater, err := s.Redis.get(repeaterId)
			if err != nil {
				klog.Errorf("Error getting repeater from Redis", err)
				return
			}
			repeater.PingsReceived++
			s.Redis.store(repeaterId, repeater)
			s.sendCommand(repeaterId, COMMAND_MSTPONG, repeaterIdBytes)
		} else {
			s.sendCommand(repeaterId, COMMAND_MSTNAK, repeaterIdBytes)
		}
	} else if command == COMMAND_RPTACK[:4] {
		klog.Warning("TODO: RPTACK")
	} else if command == COMMAND_MSTCL[:4] {
		klog.Warning("TODO: MSTCL")
	} else if command == COMMAND_MSTNAK[:4] {
		klog.Warning("TODO: MSTNAK")
	} else if command == COMMAND_MSTPONG[:4] {
		klog.Warning("TODO: MSTPONG")
	} else if command == COMMAND_MSTN {
		klog.Warning("TODO: MSTN")
	} else if command == COMMAND_MSTP {
		klog.Warning("TODO: MSTP")
	} else if command == COMMAND_MSTC {
		klog.Warning("TODO: MSTC")
	} else if command == COMMAND_RPTA {
		klog.Warning("TODO: RPTA")
	} else if command == COMMAND_RPTS {
		klog.Warning("TODO: RPTS")
	} else if command == COMMAND_RPTSBKN[:4] {
		klog.Warning("TODO: RPTSBKN")
	} else {
		klog.Warning("Unknown Command: %s", command)
	}
}
