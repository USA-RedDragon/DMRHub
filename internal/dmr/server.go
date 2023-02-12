package dmr

import (
	"context"
	"encoding/binary"
	"net"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

// Server is the DMR server.
type Server struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Started       bool
	Parrot        *Parrot
	DB            *gorm.DB
	Redis         redisRepeaterStorage
	CallTracker   *CallTracker
}

const largestMessageSize = 302
const repeaterIDLength = 4
const bufferSize = 1000000 // 1MB

// MakeServer creates a new DMR server.
func MakeServer(db *gorm.DB, redis *redis.Client) Server {
	return Server{
		Buffer: make([]byte, largestMessageSize),
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(config.GetConfig().ListenAddr),
			Port: config.GetConfig().DMRPort,
		},
		Started:     false,
		Parrot:      NewParrot(redis),
		DB:          db,
		Redis:       makeRedisRepeaterStorage(redis),
		CallTracker: NewCallTracker(db, redis),
	}
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) {
	// Send a MSTCL command to each repeater.
	repeaters, err := s.Redis.list(ctx)
	if err != nil {
		klog.Errorf("Error scanning redis for repeaters", err)
	}
	for _, repeater := range repeaters {
		if config.GetConfig().Debug {
			klog.Infof("Repeater found: %d", repeater)
		}
		s.Redis.updateConnection(ctx, repeater, "DISCONNECTED")
		repeaterBinary := make([]byte, repeaterIDLength)
		binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
		s.sendCommand(ctx, repeater, dmrconst.CommandMSTCL, repeaterBinary)
	}
	s.Started = false
}

func (s *Server) listen(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "incoming")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub", err)
		}
	}()
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

func (s *Server) send(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "outgoing")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Error unmarshalling packet", err)
			continue
		}
		_, err = s.Server.WriteToUDP(packet.Data, &net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		})
		if err != nil {
			klog.Errorf("Error sending packet", err)
		}
	}
}

func (s *Server) sendNoAddr(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "outgoing:noaddr")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		packet := models.UnpackPacket([]byte(msg.Payload))
		repeater, err := s.Redis.get(ctx, packet.Repeater)
		if err != nil {
			klog.Errorf("Error getting repeater %d from redis", packet.Repeater)
			continue
		}
		_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
			IP:   net.ParseIP(repeater.IP),
			Port: repeater.Port,
		})
		if err != nil {
			klog.Errorf("Error sending packet", err)
		}
	}
}

// Listen starts the DMR server.
func (s *Server) Listen(ctx context.Context) {
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}

	s.Server = server
	s.Started = true

	klog.Infof("DMR Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)

	go s.listen(ctx)
	go s.send(ctx)
	go s.sendNoAddr(ctx)

	go func() {
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if config.GetConfig().Debug {
				klog.Infof("Read a message from %v\n", remoteaddr)
			}
			if err != nil {
				klog.Warningf("Error reading from UDP Socket, Swallowing Error: %v", err)
				continue
			}
			go func() {
				p := models.RawDMRPacket{
					Data:       s.Buffer[:length],
					RemoteIP:   remoteaddr.IP.String(),
					RemotePort: remoteaddr.Port,
				}
				packedBytes, err := p.MarshalMsg(nil)
				if err != nil {
					klog.Errorf("Error marshalling packet", err)
					return
				}
				s.Redis.Redis.Publish(ctx, "incoming", packedBytes)
			}()
		}
	}()
}

func (s *Server) sendCommand(ctx context.Context, repeaterIDBytes uint, command dmrconst.Command, data []byte) {
	go func() {
		if !s.Started {
			klog.Warningf("Server not started, not sending command")
			return
		}
		if config.GetConfig().Debug {
			klog.Infof("Sending Command %s to Repeater ID: %d", command, repeaterIDBytes)
		}
		commandPrefixedData := append([]byte(command), data...)
		repeater, err := s.Redis.get(ctx, repeaterIDBytes)
		if err != nil {
			klog.Errorf("Error getting repeater from Redis", err)
			return
		}
		p := models.RawDMRPacket{
			Data:       commandPrefixedData,
			RemoteIP:   repeater.IP,
			RemotePort: repeater.Port,
		}
		packedBytes, err := p.MarshalMsg(nil)
		if err != nil {
			klog.Errorf("Error marshalling packet", err)
			return
		}
		s.Redis.Redis.Publish(ctx, "outgoing", packedBytes)
	}()
}

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	go func() {
		if config.GetConfig().Debug {
			klog.Infof("Sending Packet: %s\n", packet.String())
			klog.Infof("Sending DMR packet to Repeater ID: %d", repeaterIDBytes)
		}
		repeater, err := s.Redis.get(ctx, repeaterIDBytes)
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
		s.Redis.Redis.Publish(ctx, "outgoing", packedBytes)
	}()
}
