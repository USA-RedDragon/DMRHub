package main

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

	"github.com/go-redis/redis"
)

type ThreadedUDPServer struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Redis         *redis.Client
	Started       bool
	Parrot        *Parrot
}

func makeThreadedUDPServer(addr string, port int, redisHost string) ThreadedUDPServer {
	return ThreadedUDPServer{
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
	}
}

func (s ThreadedUDPServer) Stop() {
	// Send a MSTCL command to each peer
	var cursor uint64
	for {
		keys, cursor, err := s.Redis.Scan(cursor, "peer:*", 0).Result()
		handleError("Error scanning redis for peers", err)
		for _, key := range keys {
			fmt.Println("Sending MSTCL to", key)
			peernum, err := strconv.Atoi(strings.Replace(key, "peer:", "", 1))
			handleError("Error converting peer key to int", err)
			s.updatePeerConnection(peernum, "DISCONNECTED")
			peerBinary := make([]byte, 4)
			binary.BigEndian.PutUint32(peerBinary, uint32(peernum))
			s.sendCommand(peernum, COMMAND_MSTCL, peerBinary)
		}

		if cursor == 0 {
			break
		}
	}
	s.Started = false
}

func (s ThreadedUDPServer) bumpPeerPing(peerID int) {
	peer := s.getPeer(peerID)
	peer.LastPing = time.Now()
	s.storePeer(peerID, peer)
	s.Redis.Expire(fmt.Sprintf("peer:%d", peerID), 5*time.Minute)
}

func (s ThreadedUDPServer) updatePeerConnection(peerID int, connection string) {
	peer := s.getPeer(peerID)
	peer.Connection = connection
	s.storePeer(peerID, peer)
}

func (s ThreadedUDPServer) validPeer(peerID int, connection string, remoteAddr net.UDPAddr) bool {
	valid := true
	if !s.peerExists(peerID) {
		log("Peer %d does not exist", peerID)
		valid = false
	}
	peer := s.getPeer(peerID)
	if peer.IP != remoteAddr.IP.String() {
		log("Peer %d IP %s does not match remote %s", peerID, peer.IP, remoteAddr.IP.String())
		valid = false
	}
	if peer.Connection != connection {
		log("Peer %d state %s does not match expected %s", peerID, peer.Connection, connection)
		valid = false
	}
	return valid
}

func (s ThreadedUDPServer) Listen() {
	log("DMR Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	s.Server = server
	s.Started = true
	handleError("Error opening UDP Socket", err)

	for {
		len, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
		fmt.Printf("Read a message from %v\n", remoteaddr)
		if err != nil {
			log("Error reading from UDP Socket, Swallowing Error: %v", err)
			continue
		}
		go s.handlePacket(remoteaddr, s.Buffer[:len])
	}
}

func (s ThreadedUDPServer) deletePeer(peer_id int) bool {
	return s.Redis.Del(fmt.Sprintf("peer:%d", peer_id)).Val() == 1
}

func (s ThreadedUDPServer) storePeer(peer_id int, peer HomeBrewProtocolPeer) {
	peerBytes, err := peer.MarshalMsg(nil)
	handleError("Error marshalling peer", err)
	// Expire peers after 5 minutes, this function called often enough to keep them alive
	s.Redis.Set(fmt.Sprintf("peer:%d", peer_id), peerBytes, 5*time.Minute)
}

func (s ThreadedUDPServer) getPeer(peer_id int) HomeBrewProtocolPeer {
	peerBits, err := s.Redis.Get(fmt.Sprintf("peer:%d", peer_id)).Result()
	handleError("Error getting peer from redis", err)
	var peer HomeBrewProtocolPeer
	_, err = peer.UnmarshalMsg([]byte(peerBits))
	handleError("Error unmarshalling peer", err)
	return peer
}

func (s ThreadedUDPServer) sendCommand(peer_id int, command string, data []byte) {
	if !s.Started {
		log("Server not started, not sending command")
		return
	}
	log("Sending Command %s to Peer ID: %d", command, peer_id)
	command_prefixed_data := append([]byte(command), data...)
	peer := s.getPeer(peer_id)
	_, err := s.Server.WriteToUDP(command_prefixed_data, &net.UDPAddr{
		IP:   net.ParseIP(peer.IP),
		Port: peer.Port,
	})
	handleError("Error writing to UDP Socket", err)
}

func (s ThreadedUDPServer) sendPacket(peer_id int, data []byte) {
	fmt.Printf("Sending Packet: %v\n", data)
	log("Sending DMR packet to Peer ID: %d", peer_id)
	peer := s.getPeer(peer_id)
	_, err := s.Server.WriteToUDP(data, &net.UDPAddr{
		IP:   net.ParseIP(peer.IP),
		Port: peer.Port,
	})
	handleError("Error writing to UDP Socket", err)
}

func (s ThreadedUDPServer) peerExists(peer_id int) bool {
	return s.Redis.Exists(fmt.Sprintf("peer:%d", peer_id)).Val() == 1
}

func (s ThreadedUDPServer) handlePacket(remoteAddr *net.UDPAddr, data []byte) {
	log("Handling Packet from %v", remoteAddr)
	log("Data: %s", string(data[:]))
	// Extract the command, which is various length, all but one 4 significant characters -- RPTCL
	command := string(data[:4])
	if command == COMMAND_DMRA {
		peer_id := data[4:8]
		peer_id_int := int(binary.BigEndian.Uint32(peer_id))
		log("DMR talk alias from Peer ID: %d", peer_id_int)
		if s.validPeer(peer_id_int, "YES", *remoteAddr) {
			s.bumpPeerPing(peer_id_int)
			log("TODO: DMRA")
		}
	} else if command == COMMAND_DMRD {
		peer_id := data[11:15]
		peer_id_int := int(binary.BigEndian.Uint32(peer_id))
		log("DMR Data from Peer ID: %d", peer_id_int)
		if s.validPeer(peer_id_int, "YES", *remoteAddr) {
			s.bumpPeerPing(peer_id_int)
			seq := data[4]
			rfSrc := int(data[5])<<16 | int(data[6])<<8 | int(data[7])
			dstID := int(data[8])<<16 | int(data[9])<<8 | int(data[10])
			if len(data) < 16 {
				return
			}
			bits := data[15]
			slot := 0
			if (bits & 0x80) != 0 {
				slot = 2
			} else {
				slot = 1
			}
			call_type := ""
			if (bits & 0x40) != 0 {
				call_type = "unit"
			} else if (bits & 0x23) == 0x23 {
				call_type = "vcsbk"
			} else {
				call_type = "group"
			}
			frame_type := (bits & 0x30) >> 4
			dtype_vseq := (bits & 0xF) // data, 1=voice header, 2=voice terminator; voice, 0=burst A ... 5=burst F
			stream_id := int(data[16])<<24 | int(data[17])<<16 | int(data[18])<<8 | int(data[19])
			fmt.Printf("DMR Data: seq %d rfSrc %d dstID %d bits %d slot %d call_type %s frame_type %d dtype_vseq %d stream_id %d\n", seq, rfSrc, dstID, bits, slot, call_type, frame_type, dtype_vseq, stream_id)
			switch frame_type {
			case HBPF_DATA_SYNC:
				log("BLUG data sync")
				break
			case HBPF_VOICE_SYNC:
				log("BLUG voice sync")
				break
			case HBPF_VOICE:
				log("BLUG voice")
				break
			}
			log("BLUG dtype_vseq %d", dtype_vseq)
			if dstID == 9990 {
				if call_type == "unit" {
					log("Parrot call from %d", rfSrc)
					if !s.Parrot.isStarted(stream_id) {
						s.Parrot.startStream(stream_id, peer_id_int)
					}
					s.Parrot.recordPacket(stream_id, data)
					if frame_type == HBPF_DATA_SYNC && dtype_vseq == HBPF_SLT_VTERM {
						s.Parrot.stopStream(stream_id)
						f := func() {
							packets := s.Parrot.getStream(stream_id)
							time.Sleep(3 * time.Second)
							for _, packet := range packets {
								s.sendPacket(peer_id_int, packet)
								// Just enough delay to avoid overloading the peer host
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

			if dstID == 4000 {
				// TODO: Handle unlink
				log("TODO: unlink")
				return
			}

			if call_type == "group" {
				// For each peer in Redis
				var cursor uint64
				for {
					keys, cursor, err := s.Redis.Scan(cursor, "peer:*", 0).Result()
					handleError("Error scanning redis for peers", err)
					for _, key := range keys {
						if key != fmt.Sprintf("peer:%d", peer_id_int) {
							fmt.Println("Peer found: ", key)
							peernum, err := strconv.Atoi(strings.Replace(key, "peer:", "", 1))
							handleError("Error converting peer key to int", err)
							peerBinary := make([]byte, 4)
							binary.BigEndian.PutUint32(peerBinary, uint32(peernum))
							pkt := append(data[:11], peerBinary[:4]...)
							pkt = append(pkt, data[15:]...)
							s.sendPacket(peernum, pkt)
						}
					}

					if cursor == 0 {
						break
					}
				}
			} else if call_type == "unit" {
				if s.peerExists(dstID) {
					peerBinary := make([]byte, 4)
					binary.BigEndian.PutUint32(peerBinary, uint32(dstID))
					pkt := append(data[:11], peerBinary[:4]...)
					pkt = append(pkt, data[15:]...)
					s.sendPacket(peer_id_int, pkt)
				} else {
					log("Unit call to non-existent peer %d", dstID)
				}
			}
		}
	} else if command == COMMAND_RPTO {
		peer_id := data[4:8]
		peer_id_int := int(binary.BigEndian.Uint32(peer_id))
		log("Set options from %d", peer_id_int)

		if s.validPeer(peer_id_int, "YES", *remoteAddr) {
			s.bumpPeerPing(peer_id_int)
			log("TODO: RPTO")
			s.sendCommand(peer_id_int, COMMAND_RPTACK, peer_id)
		}
	} else if command == COMMAND_RPTL {
		peer_id := data[4:8]
		peer_id_int := int(binary.BigEndian.Uint32(peer_id))
		log("Login from Peer ID: %d", peer_id_int)
		// TODO: Validate radio ID
		if valid := true; !valid {
			s.sendCommand(peer_id_int, COMMAND_MSTNAK, peer_id)
			log("Peer ID %d is not valid, sending NAK", peer_id_int)
		} else {
			bigSalt, err := rand.Int(rand.Reader, big.NewInt(0xFFFFFFFF))
			handleError("Error generating random salt", err)
			peer := makePeer(peer_id_int, uint32(bigSalt.Uint64()), *remoteAddr)
			peer.Connection = "RPTL-RECEIVED"
			peer.LastPing = time.Now()
			peer.Connected = time.Now()
			s.storePeer(peer_id_int, peer)
			s.sendCommand(peer_id_int, COMMAND_RPTACK, bigSalt.Bytes()[0:4])
			s.updatePeerConnection(peer_id_int, "CHALLENGE_SENT")
		}
	} else if command == COMMAND_RPTK {
		peer_id := data[4:8]
		peer_id_int := int(binary.BigEndian.Uint32(peer_id))
		log("Challenge Response from Peer ID: %d", peer_id_int)
		if s.validPeer(peer_id_int, "CHALLENGE_SENT", *remoteAddr) {
			s.bumpPeerPing(peer_id_int)
			peer := s.getPeer(peer_id_int)
			rxSalt := binary.BigEndian.Uint32(data[8:])
			// sha256 hash peer.Salt + the passphrase
			saltBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(saltBytes, peer.Salt)
			hash := sha256.Sum256(append(saltBytes, []byte("s3cr37w0rd")...))
			calcedSalt := binary.BigEndian.Uint32(hash[:])
			if calcedSalt == rxSalt {
				log("Peer ID %d authed, sending ACK", peer_id_int)
				s.updatePeerConnection(peer_id_int, "WAITING_CONFIG")
				s.sendCommand(peer_id_int, COMMAND_RPTACK, peer_id)
			} else {
				s.sendCommand(peer_id_int, COMMAND_MSTNAK, peer_id)
			}
		} else {
			s.sendCommand(peer_id_int, COMMAND_MSTNAK, peer_id)
		}
	} else if command == COMMAND_RPTC {
		if string(data[:5]) == COMMAND_RPTCL {
			peer_id := data[5:9]
			peer_id_int := int(binary.BigEndian.Uint32(peer_id))
			log("Disconnect from Peer ID: %d", peer_id_int)
			if s.validPeer(peer_id_int, "YES", *remoteAddr) {
				s.sendCommand(peer_id_int, COMMAND_MSTNAK, peer_id)
			}
			if !s.deletePeer(peer_id_int) {
				log("Peer ID %d not deleted", peer_id_int)
			}
		} else {
			peer_id := data[4:8]
			peer_id_int := int(binary.BigEndian.Uint32(peer_id))
			log("Repeater config from %d", peer_id_int)

			if s.validPeer(peer_id_int, "WAITING_CONFIG", *remoteAddr) {
				s.bumpPeerPing(peer_id_int)
				peer := s.getPeer(peer_id_int)
				peer.Connected = time.Now()
				peer.LastPing = time.Now()

				peer.Callsign = strings.TrimRight(string(data[8:16]), " ")
				rxFreq, err := strconv.ParseInt(strings.TrimRight(string(data[16:25]), " "), 0, 32)
				handleError("Error parsing RXFreq", err)
				peer.RXFrequency = int(rxFreq)
				txFreq, err := strconv.ParseInt(strings.TrimRight(string(data[25:34]), " "), 0, 32)
				handleError("Error parsing TXFreq", err)
				peer.TXFrequency = int(txFreq)
				txPower, err := strconv.ParseInt(strings.TrimRight(string(data[34:36]), " "), 0, 32)
				handleError("Error parsing TXPower", err)
				peer.TXPower = int(txPower)
				colorCode, err := strconv.ParseInt(strings.TrimRight(string(data[36:38]), " "), 0, 32)
				handleError("Error parsing ColorCode", err)
				peer.ColorCode = int(colorCode)
				lat, err := strconv.ParseFloat(strings.TrimRight(string(data[38:46]), " "), 32)
				handleError("Error parsing Latitude", err)
				peer.Latitude = float32(lat)
				long, err := strconv.ParseFloat(strings.TrimRight(string(data[46:55]), " "), 32)
				handleError("Error parsing Longitude", err)
				peer.Longitude = float32(long)
				height, err := strconv.ParseInt(strings.TrimRight(string(data[55:58]), " "), 0, 32)
				handleError("Error parsing Height", err)
				peer.Height = int(height)
				peer.Location = strings.TrimRight(string(data[58:78]), " ")
				peer.Description = strings.TrimRight(string(data[78:97]), " ")
				slots, err := strconv.ParseInt(strings.TrimRight(string(data[97:98]), " "), 0, 32)
				handleError("Error parsing Slots", err)
				peer.Slots = int(slots)
				peer.URL = strings.TrimRight(string(data[98:222]), " ")
				peer.SoftwareID = strings.TrimRight(string(data[222:262]), " ")
				peer.PackageID = strings.TrimRight(string(data[262:302]), " ")
				peer.Connection = "YES"
				s.storePeer(peer_id_int, peer)
				fmt.Printf("Peer ID %d (%s) connected\n", peer_id_int, peer.Callsign)
				s.sendCommand(peer_id_int, COMMAND_RPTACK, peer_id)
			} else {
				s.sendCommand(peer_id_int, COMMAND_MSTNAK, peer_id)
			}
		}
	} else if command == COMMAND_RPTP {
		peer_id := data[7:11]
		peer_id_int := int(binary.BigEndian.Uint32(peer_id))
		log("Ping from %d", peer_id_int)

		if s.validPeer(peer_id_int, "YES", *remoteAddr) {
			s.bumpPeerPing(peer_id_int)
			peer := s.getPeer(peer_id_int)
			peer.PingsReceived++
			s.storePeer(peer_id_int, peer)
			s.sendCommand(peer_id_int, COMMAND_MSTPONG, peer_id)
		} else {
			s.sendCommand(peer_id_int, COMMAND_MSTNAK, peer_id)
		}
	} else if command == COMMAND_RPTACK[:4] {
		log("TODO: RPTACK")
	} else if command == COMMAND_MSTCL[:4] {
		log("TODO: MSTCL")
	} else if command == COMMAND_MSTNAK[:4] {
		log("TODO: MSTNAK")
	} else if command == COMMAND_MSTPONG[:4] {
		log("TODO: MSTPONG")
	} else if command == COMMAND_MSTN {
		log("TODO: MSTN")
	} else if command == COMMAND_MSTP {
		log("TODO: MSTP")
	} else if command == COMMAND_MSTC {
		log("TODO: MSTC")
	} else if command == COMMAND_RPTA {
		log("TODO: RPTA")
	} else if command == COMMAND_RPTS {
		log("TODO: RPTS")
	} else if command == COMMAND_RPTSBKN[:4] {
		log("TODO: RPTSBKN")
	} else {
		log("Unknown Command: %s", command)
	}
}
