package dmr

import (
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/go-redis/redis"
	"k8s.io/klog/v2"
)

type Parrot struct {
	Redis redisParrotStorage
}

func NewParrot(redis *redis.Client) *Parrot {
	return &Parrot{
		Redis: makeRedisParrotStorage(redis),
	}
}

func (p *Parrot) IsStarted(streamId uint) bool {
	return p.Redis.exists(streamId)
}

func (p *Parrot) StartStream(streamId uint, repeaterId uint) bool {
	if !p.Redis.exists(streamId) {
		p.Redis.store(streamId, repeaterId)
		return true
	}
	klog.Warningf("Parrot: Stream %d already started", streamId)
	return false
}

func (p *Parrot) RecordPacket(streamId uint, packet models.Packet) {
	go p.Redis.refresh(streamId)

	// Grab the repeater ID to go ahead and mark the packet as being routed back
	repeaterId, err := p.Redis.get(streamId)
	if err != nil {
		klog.Errorf("Error getting parrot stream from redis", err)
		return
	}

	packet.Repeater = repeaterId
	tmp_src := packet.Src
	packet.Src = packet.Dst
	packet.Dst = tmp_src
	packet.GroupCall = false
	packet.BER = -1
	packet.RSSI = -1

	p.Redis.stream(streamId, packet)
}

func (p *Parrot) StopStream(streamId uint) {
	p.Redis.delete(streamId)
}

func (p *Parrot) GetStream(streamId uint) []models.Packet {
	// Empty array of packet byte arrays
	packets, err := p.Redis.getStream(streamId)
	if err != nil {
		klog.Errorf("Error getting parrot stream from redis: %s", err)
		return nil
	}

	return packets
}
