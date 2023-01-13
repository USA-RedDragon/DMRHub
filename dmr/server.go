package dmr

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/go-redis/redis"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type DMRServer struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Redis         *redis.Client
	Started       bool
	Parrot        *Parrot
	Verbose       bool
	DB            *gorm.DB
}

func MakeServer(addr string, port int, redisHost string, verbose bool, db *gorm.DB) DMRServer {
	return DMRServer{
		Buffer: make([]byte, 4096),
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(addr),
			Port: port,
		},
		Redis: redis.NewClient(&redis.Options{
			Addr: redisHost,
		}),
		Started: false,
		Parrot:  newParrot(redisHost),
		Verbose: verbose,
		DB:      db,
	}
}

func (s DMRServer) Stop() {
	// Send a MSTCL command to each repeater
	var cursor uint64
	for {
		keys, cursor, err := s.Redis.Scan(cursor, "repeater:*", 0).Result()
		if err != nil {
			klog.Errorf("Error scanning redis for repeaters", err)
			break
		}
		for _, key := range keys {
			if s.Verbose {
				klog.Info("Sending MSTCL to", key)
			}
			repeaterNum, err := strconv.Atoi(strings.Replace(key, "repeater:", "", 1))
			if err != nil {
				klog.Errorf("Error converting repeater key to int", err)
			}
			s.updateRepeaterConnection(repeaterNum, "DISCONNECTED")
			repeaterBinary := make([]byte, 4)
			binary.BigEndian.PutUint32(repeaterBinary, uint32(repeaterNum))
			s.sendCommand(repeaterNum, COMMAND_MSTCL, repeaterBinary)
		}

		if cursor == 0 {
			break
		}
	}
	s.Started = false
}

func (s DMRServer) bumpRepeaterPing(repeaterID int) {
	repeater := s.getRepeater(repeaterID)
	repeater.LastPing = time.Now()
	s.storeRepeater(repeaterID, repeater)
	s.Redis.Expire(fmt.Sprintf("repeater:%d", repeaterID), 5*time.Minute)
}

func (s DMRServer) updateRepeaterConnection(repeaterID int, connection string) {
	repeater := s.getRepeater(repeaterID)
	repeater.Connection = connection
	s.storeRepeater(repeaterID, repeater)
}

func (s DMRServer) validRepeater(repeaterID int, connection string, remoteAddr net.UDPAddr) bool {
	valid := true
	if !s.repeaterExists(repeaterID) {
		klog.Warningf("Repeater %d does not exist", repeaterID)
		valid = false
	}
	repeater := s.getRepeater(repeaterID)
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
		go s.handlePacket(remoteaddr, s.Buffer[:len])
	}
}

func (s DMRServer) deleteRepeater(repeaterId int) bool {
	return s.Redis.Del(fmt.Sprintf("repeater:%d", repeaterId)).Val() == 1
}

func (s DMRServer) storeRepeater(repeaterId int, repeater models.Repeater) {
	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		klog.Errorf("Error marshalling repeater", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	s.Redis.Set(fmt.Sprintf("repeater:%d", repeaterId), repeaterBytes, 5*time.Minute)
}

func (s DMRServer) getRepeater(repeaterId int) models.Repeater {
	repeaterBits, err := s.Redis.Get(fmt.Sprintf("repeater:%d", repeaterId)).Result()
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
	}
	var repeater models.Repeater
	_, err = repeater.UnmarshalMsg([]byte(repeaterBits))
	if err != nil {
		klog.Errorf("Error unmarshalling repeater", err)
		return models.Repeater{}
	}
	return repeater
}

func (s DMRServer) sendCommand(repeaterId int, command string, data []byte) {
	if !s.Started {
		klog.Warningf("Server not started, not sending command")
		return
	}
	if s.Verbose {
		klog.Infof("Sending Command %s to Repeater ID: %d", command, repeaterId)
	}
	command_prefixed_data := append([]byte(command), data...)
	repeater := s.getRepeater(repeaterId)
	_, err := s.Server.WriteToUDP(command_prefixed_data, &net.UDPAddr{
		IP:   net.ParseIP(repeater.IP),
		Port: repeater.Port,
	})
	if err != nil {
		klog.Errorf("Error writing to UDP Socket", err)
	}
}

func (s DMRServer) sendPacket(repeaterId int, data []byte) {
	if s.Verbose {
		klog.Infof("Sending Packet: %v\n", data)
		klog.Infof("Sending DMR packet to Repeater ID: %d", repeaterId)
	}
	repeater := s.getRepeater(repeaterId)
	_, err := s.Server.WriteToUDP(data, &net.UDPAddr{
		IP:   net.ParseIP(repeater.IP),
		Port: repeater.Port,
	})
	if err != nil {
		klog.Errorf("Error writing to UDP Socket", err)
	}
}

func (s DMRServer) repeaterExists(repeaterId int) bool {
	return s.Redis.Exists(fmt.Sprintf("repeater:%d", repeaterId)).Val() == 1
}

func (s DMRServer) handlePacket(remoteAddr *net.UDPAddr, data []byte) {
	klog.Infof("Handling Packet from %v", remoteAddr)
	if s.Verbose {
		klog.Infof("Data: %s", string(data[:]))
	}
	// Extract the command, which is various length, all but one 4 significant characters -- RPTCL
	command := string(data[:4])
	if command == COMMAND_DMRA {
		repeaterId := data[4:8]
		repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
		if s.Verbose {
			klog.Infof("DMR talk alias from Repeater ID: %d", repeaterIdInt)
		}
		if s.validRepeater(repeaterIdInt, "YES", *remoteAddr) {
			s.bumpRepeaterPing(repeaterIdInt)
			klog.Warning("TODO: DMRA")
		}
	} else if command == COMMAND_DMRD {
		repeaterId := data[11:15]
		repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
		if s.Verbose {
			klog.Infof("DMR Data from Repeater ID: %d", repeaterIdInt)
		}
		if s.validRepeater(repeaterIdInt, "YES", *remoteAddr) {
			s.bumpRepeaterPing(repeaterIdInt)
			packet := models.UnpackPacket(data[:])
			if s.Verbose {
				klog.Infof("DMR Data: %v", packet)
				switch int(packet.FrameType) {
				case HBPF_DATA_SYNC:
					klog.Info("BLUG data sync")
					break
				case HBPF_VOICE_SYNC:
					klog.Info("BLUG voice sync")
					break
				case HBPF_VOICE:
					klog.Info("BLUG voice")
					break
				}
				klog.Infof("BLUG dtype_vseq %d", packet.DTypeOrVSeq)
			}
			if packet.Dst == 9990 {
				if !packet.GroupCall {
					klog.Infof("Parrot call from %d", packet.Src)
					if !s.Parrot.isStarted(packet.StreamId) {
						s.Parrot.startStream(packet.StreamId, repeaterIdInt)
					}
					s.Parrot.recordPacket(packet.StreamId, packet)
					if packet.FrameType == HBPF_DATA_SYNC && packet.DTypeOrVSeq == HBPF_SLT_VTERM {
						s.Parrot.stopStream(packet.StreamId)
						f := func() {
							packets := s.Parrot.getStream(packet.StreamId)
							time.Sleep(3 * time.Second)
							for _, packet := range packets {
								s.sendPacket(repeaterIdInt, packet)
								// Just enough delay to avoid overloading the repeater host
								time.Sleep(60 * time.Millisecond)
							}
						}
						go f()
					}
					return
				} else {
					// Don't route parrot group calls
					return
				}
			}

			if packet.Dst == 4000 {
				// TODO: Handle unlink
				klog.Warning("TODO: unlink")
				return
			}

			if packet.GroupCall {
				// For each repeater in Redis
				var cursor uint64
				for {
					keys, cursor, err := s.Redis.Scan(cursor, "repeater:*", 0).Result()
					if err != nil {
						klog.Errorf("Error scanning redis for repeaters", err)
						break
					}
					for _, key := range keys {
						if key != fmt.Sprintf("repeater:%d", repeaterIdInt) {
							if s.Verbose {
								klog.Infof("Repeater found: %s", key)
							}
							repeaterNum, err := strconv.Atoi(strings.Replace(key, "repeater:", "", 1))
							if err != nil {
								klog.Errorf("Error converting repeater key to int", err)
							}
							repeaterBinary := make([]byte, 4)
							binary.BigEndian.PutUint32(repeaterBinary, uint32(repeaterNum))
							pkt := append(data[:11], repeaterBinary[:4]...)
							pkt = append(pkt, data[15:]...)
							s.sendPacket(repeaterNum, pkt)
						}
					}

					if cursor == 0 {
						break
					}
				}
			} else {
				if s.repeaterExists(int(packet.Dst)) {
					repeaterBinary := make([]byte, 4)
					binary.BigEndian.PutUint32(repeaterBinary, uint32(packet.Dst))
					pkt := append(data[:11], repeaterBinary[:4]...)
					pkt = append(pkt, data[15:]...)
					s.sendPacket(repeaterIdInt, pkt)
				} else {
					klog.Warning("Unit call to non-existent repeater %d", packet.Dst)
				}
			}
		}
	} else if command == COMMAND_RPTO {
		repeaterId := data[4:8]
		repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
		if s.Verbose {
			klog.Infof("Set options from %d", repeaterIdInt)
		}

		if s.validRepeater(repeaterIdInt, "YES", *remoteAddr) {
			s.bumpRepeaterPing(repeaterIdInt)
			klog.Warning("TODO: RPTO")
			s.sendCommand(repeaterIdInt, COMMAND_RPTACK, repeaterId)
		}
	} else if command == COMMAND_RPTL {
		repeaterId := data[4:8]
		repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
		klog.Infof("Login from Repeater ID: %d", repeaterIdInt)
		// TODO: Validate radio ID
		if valid := true; !valid {
			s.sendCommand(repeaterIdInt, COMMAND_MSTNAK, repeaterId)
			if s.Verbose {
				klog.Infof("Repeater ID %d is not valid, sending NAK", repeaterIdInt)
			}
		} else {
			bigSalt, err := rand.Int(rand.Reader, big.NewInt(0xFFFFFFFF))
			if err != nil {
				klog.Exitf("Error generating random salt", err)
			}
			repeater := models.MakeRepeater(repeaterIdInt, uint32(bigSalt.Uint64()), *remoteAddr)
			repeater.Connection = "RPTL-RECEIVED"
			repeater.LastPing = time.Now()
			repeater.Connected = time.Now()
			s.storeRepeater(repeaterIdInt, repeater)
			s.sendCommand(repeaterIdInt, COMMAND_RPTACK, bigSalt.Bytes()[0:4])
			s.updateRepeaterConnection(repeaterIdInt, "CHALLENGE_SENT")
		}
	} else if command == COMMAND_RPTK {
		repeaterId := data[4:8]
		repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
		if s.Verbose {
			klog.Infof("Challenge Response from Repeater ID: %d", repeaterIdInt)
		}
		if s.validRepeater(repeaterIdInt, "CHALLENGE_SENT", *remoteAddr) {
			s.bumpRepeaterPing(repeaterIdInt)
			repeater := s.getRepeater(repeaterIdInt)
			rxSalt := binary.BigEndian.Uint32(data[8:])
			// sha256 hash repeater.Salt + the passphrase
			saltBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(saltBytes, repeater.Salt)
			hash := sha256.Sum256(append(saltBytes, []byte("s3cr37w0rd")...))
			calcedSalt := binary.BigEndian.Uint32(hash[:])
			if calcedSalt == rxSalt {
				klog.Infof("Repeater ID %d authed, sending ACK", repeaterIdInt)
				s.updateRepeaterConnection(repeaterIdInt, "WAITING_CONFIG")
				s.sendCommand(repeaterIdInt, COMMAND_RPTACK, repeaterId)
			} else {
				s.sendCommand(repeaterIdInt, COMMAND_MSTNAK, repeaterId)
			}
		} else {
			s.sendCommand(repeaterIdInt, COMMAND_MSTNAK, repeaterId)
		}
	} else if command == COMMAND_RPTC {
		if string(data[:5]) == COMMAND_RPTCL {
			repeaterId := data[5:9]
			repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
			klog.Infof("Disconnect from Repeater ID: %d", repeaterIdInt)
			if s.validRepeater(repeaterIdInt, "YES", *remoteAddr) {
				s.sendCommand(repeaterIdInt, COMMAND_MSTNAK, repeaterId)
			}
			if !s.deleteRepeater(repeaterIdInt) {
				klog.Warningf("Repeater ID %d not deleted", repeaterIdInt)
			}
		} else {
			repeaterId := data[4:8]
			repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
			if s.Verbose {
				klog.Infof("Repeater config from %d", repeaterIdInt)
			}

			if s.validRepeater(repeaterIdInt, "WAITING_CONFIG", *remoteAddr) {
				s.bumpRepeaterPing(repeaterIdInt)
				repeater := s.getRepeater(repeaterIdInt)
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
				repeater.ColorCode = int(colorCode)
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
				s.storeRepeater(repeaterIdInt, repeater)
				klog.Infof("Repeater ID %d (%s) connected\n", repeaterIdInt, repeater.Callsign)
				s.sendCommand(repeaterIdInt, COMMAND_RPTACK, repeaterId)
			} else {
				s.sendCommand(repeaterIdInt, COMMAND_MSTNAK, repeaterId)
			}
		}
	} else if command == COMMAND_RPTP {
		repeaterId := data[7:11]
		repeaterIdInt := int(binary.BigEndian.Uint32(repeaterId))
		klog.Infof("Ping from %d", repeaterIdInt)

		if s.validRepeater(repeaterIdInt, "YES", *remoteAddr) {
			s.bumpRepeaterPing(repeaterIdInt)
			repeater := s.getRepeater(repeaterIdInt)
			repeater.PingsReceived++
			s.storeRepeater(repeaterIdInt, repeater)
			s.sendCommand(repeaterIdInt, COMMAND_MSTPONG, repeaterId)
		} else {
			s.sendCommand(repeaterIdInt, COMMAND_MSTNAK, repeaterId)
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
