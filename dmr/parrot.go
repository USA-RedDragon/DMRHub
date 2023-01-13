package dmr

import (
	"fmt"
	"strconv"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/go-redis/redis"
	"k8s.io/klog/v2"
)

type Parrot struct {
	Redis *redis.Client
}

func newParrot(redisHost string) *Parrot {
	return &Parrot{
		Redis: redis.NewClient(&redis.Options{
			Addr: redisHost,
		}),
	}
}

func (p *Parrot) startStream(streamId uint, repeaterId int) bool {
	if !p.isStarted(streamId) {
		p.Redis.Set(fmt.Sprintf("parrot:stream:%d", streamId), repeaterId, 5*time.Minute)
		return true
	}
	klog.Warningf("Parrot: Stream %d already started", streamId)
	return false
}

func (p *Parrot) isStarted(streamId uint) bool {
	return p.Redis.Exists(fmt.Sprintf("parrot:stream:%d", streamId)).Val() == 1
}

func (p *Parrot) recordPacket(streamId uint, packet models.Packet) {
	p.Redis.Expire(fmt.Sprintf("parrot:stream:%d", streamId), 5*time.Minute)

	// Grab the repeater ID to go ahead and mark the packet as being routed back
	repeaterIdStr, err := p.Redis.Get(fmt.Sprintf("parrot:stream:%d", streamId)).Result()
	if err != nil {
		klog.Errorf("Error getting parrot stream from redis", err)
		return
	}
	repeaterId, err := strconv.Atoi(repeaterIdStr)
	if err != nil {
		klog.Errorf("Error parsing parrot stream from redis", err)
		return
	}

	packet.Repeater = uint(repeaterId)
	tmp_src := packet.Src
	packet.Src = packet.Dst
	packet.Dst = tmp_src

	p.Redis.RPush(fmt.Sprintf("parrot:stream:%d:packets", streamId), packet.Encode())
}

func (p *Parrot) stopStream(streamId uint) {
	p.Redis.Del(fmt.Sprintf("parrot:stream:%d", streamId))
	p.Redis.Expire(fmt.Sprintf("parrot:stream:%d:packets", streamId), 5*time.Minute)
}

func (p *Parrot) getStream(streamId uint) [][]byte {
	// Empty array of packet byte arrays
	var packets [][]byte
	packetSize, err := p.Redis.LLen(fmt.Sprintf("parrot:stream:%d:packets", streamId)).Result()
	if err != nil {
		klog.Errorf("Error getting parrot stream from redis", err)
		return nil
	}
	// Loop through the packets and add them to the array
	for i := int64(0); i < packetSize; i++ {
		packet, err := p.Redis.LIndex(fmt.Sprintf("parrot:stream:%d:packets", streamId), i).Bytes()
		if err != nil {
			klog.Errorf("Error getting parrot stream from redis", err)
			return nil
		}
		packets = append(packets, packet)
	}
	// Delete the stream
	p.Redis.Del(fmt.Sprintf("parrot:stream:%d:packets", streamId))
	return packets
}
