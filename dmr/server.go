package dmr

import (
	"encoding/binary"
	"net"

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
		CallTracker: NewCallTracker(redisHost, db),
	}
}

func (s *DMRServer) Stop() {
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

func (s *DMRServer) validRepeater(repeaterID uint, connection string, remoteAddr net.UDPAddr) bool {
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
func (s *DMRServer) listen() {
	pubsub := s.Redis.Redis.Subscribe("incoming")
	defer pubsub.Close()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Error unmarshalling packet", err)
			continue
		}
		s.handlePacket(&net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		}, packet.Data)
	}
}

func (s *DMRServer) send() {
	pubsub := s.Redis.Redis.Subscribe("outgoing")
	defer pubsub.Close()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Error unmarshalling packet", err)
			continue
		}
		s.Server.WriteToUDP(packet.Data, &net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		})
	}
}

func (s *DMRServer) Listen() {
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	// 1MB buffers, say what?
	server.SetReadBuffer(1000000)
	server.SetWriteBuffer(1000000)
	s.Server = server
	s.Started = true
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}
	klog.Infof("DMR Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)

	go s.listen()
	go s.send()

	go func() {
		for {
			len, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if s.Verbose {
				klog.Infof("Read a message from %v\n", remoteaddr)
			}
			if err != nil {
				klog.Warningf("Error reading from UDP Socket, Swallowing Error: %v", err)
				continue
			}
			go func() {
				p := models.RawDMRPacket{
					Data:       s.Buffer[:len],
					RemoteIP:   remoteaddr.IP.String(),
					RemotePort: remoteaddr.Port,
				}
				packedBytes, err := p.MarshalMsg(nil)
				if err != nil {
					klog.Errorf("Error marshalling packet", err)
					return
				}
				s.Redis.Redis.Publish("incoming", packedBytes)
			}()
		}
	}()
}

func (s *DMRServer) sendCommand(repeaterIdBytes uint, command string, data []byte) {
	go func() {
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
		p := models.RawDMRPacket{
			Data:       command_prefixed_data,
			RemoteIP:   repeater.IP,
			RemotePort: repeater.Port,
		}
		packedBytes, err := p.MarshalMsg(nil)
		if err != nil {
			klog.Errorf("Error marshalling packet", err)
			return
		}
		s.Redis.Redis.Publish("outgoing", packedBytes)
	}()
}

func (s *DMRServer) sendPacket(repeaterIdBytes uint, packet models.Packet) {
	go func() {
		if s.Verbose {
			klog.Infof("Sending Packet: %v\n", packet)
			klog.Infof("Sending DMR packet to Repeater ID: %d", repeaterIdBytes)
		}
		repeater, err := s.Redis.get(repeaterIdBytes)
		if err != nil {
			klog.Errorf("Error getting repeater from Redis", err)
			return
		}
		p := models.RawDMRPacket{
			Data:       packet.Encode(),
			RemoteIP:   repeater.IP,
			RemotePort: repeater.Port,
		}
		packedBytes, err := p.MarshalMsg(nil)
		if err != nil {
			klog.Errorf("Error marshalling packet", err)
			return
		}
		s.Redis.Redis.Publish("outgoing", packedBytes)
	}()
}
