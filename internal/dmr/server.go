package dmr

import (
	"context"
	"encoding/binary"
	"net"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/redis/go-redis/v9"
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

func MakeServer(db *gorm.DB, redis *redis.Client) DMRServer {
	return DMRServer{
		Buffer: make([]byte, 302),
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

func (s *DMRServer) Stop(ctx context.Context) {
	// Send a MSTCL command to each repeater
	repeaters, err := s.Redis.list(ctx)
	if err != nil {
		klog.Errorf("Error scanning redis for repeaters", err)
	}
	for _, repeater := range repeaters {
		if s.Verbose {
			klog.Infof("Repeater found: %d", repeater)
		}
		s.Redis.updateConnection(ctx, repeater, "DISCONNECTED")
		repeaterBinary := make([]byte, 4)
		binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
		s.sendCommand(ctx, repeater, dmrconst.COMMAND_MSTCL, repeaterBinary)
	}
	s.Started = false
}

func (s *DMRServer) listen(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "incoming")
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

func (s *DMRServer) send(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "outgoing")
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

func (s *DMRServer) sendNoAddr(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "outgoing:noaddr")
	defer pubsub.Close()
	for msg := range pubsub.Channel() {
		packet := models.UnpackPacket([]byte(msg.Payload))
		repeater, err := s.Redis.get(ctx, packet.Repeater)
		if err != nil {
			klog.Errorf("Error getting repeater %d from redis", packet.Repeater)
			continue
		}
		s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
			IP:   net.ParseIP(repeater.IP),
			Port: repeater.Port,
		})
	}
}

func (s *DMRServer) Listen(ctx context.Context) {
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

	go s.listen(ctx)
	go s.send(ctx)
	go s.sendNoAddr(ctx)

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
				s.Redis.Redis.Publish(ctx, "incoming", packedBytes)
			}()
		}
	}()
}

func (s *DMRServer) sendCommand(ctx context.Context, repeaterIdBytes uint, command dmrconst.Command, data []byte) {
	go func() {
		if !s.Started {
			klog.Warningf("Server not started, not sending command")
			return
		}
		if s.Verbose {
			klog.Infof("Sending Command %s to Repeater ID: %d", command, repeaterIdBytes)
		}
		command_prefixed_data := append([]byte(command), data...)
		repeater, err := s.Redis.get(ctx, repeaterIdBytes)
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
		s.Redis.Redis.Publish(ctx, "outgoing", packedBytes)
	}()
}

func (s *DMRServer) sendPacket(ctx context.Context, repeaterIdBytes uint, packet models.Packet) {
	go func() {
		if s.Verbose {
			klog.Infof("Sending Packet: %v\n", packet)
			klog.Infof("Sending DMR packet to Repeater ID: %d", repeaterIdBytes)
		}
		repeater, err := s.Redis.get(ctx, repeaterIdBytes)
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
