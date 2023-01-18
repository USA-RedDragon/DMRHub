package dmr

import (
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"k8s.io/klog/v2"
)

type Parrot struct {
	Redis redisParrotStorage
}

func NewParrot(redisHost string) *Parrot {
	return &Parrot{
		Redis: makeRedisParrotStorage(redisHost),
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
	p.Redis.refresh(streamId)

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
