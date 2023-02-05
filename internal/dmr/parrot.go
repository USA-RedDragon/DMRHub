package dmr

import (
	"context"

	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/redis/go-redis/v9"
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

func (p *Parrot) IsStarted(ctx context.Context, streamId uint) bool {
	return p.Redis.exists(ctx, streamId)
}

func (p *Parrot) StartStream(ctx context.Context, streamId uint, repeaterId uint) bool {
	if !p.Redis.exists(ctx, streamId) {
		p.Redis.store(ctx, streamId, repeaterId)
		return true
	}
	klog.Warningf("Parrot: Stream %d already started", streamId)
	return false
}

func (p *Parrot) RecordPacket(ctx context.Context, streamId uint, packet models.Packet) {
	go p.Redis.refresh(ctx, streamId)

	// Grab the repeater ID to go ahead and mark the packet as being routed back
	repeaterId, err := p.Redis.get(ctx, streamId)
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

	err = p.Redis.stream(ctx, streamId, packet)
	if err != nil {
		klog.Errorf("Error storing parrot stream in redis", err)
	}
}

func (p *Parrot) StopStream(ctx context.Context, streamId uint) {
	p.Redis.delete(ctx, streamId)
}

func (p *Parrot) GetStream(ctx context.Context, streamId uint) []models.Packet {
	// Empty array of packet byte arrays
	packets, err := p.Redis.getStream(ctx, streamId)
	if err != nil {
		klog.Errorf("Error getting parrot stream from redis: %s", err)
		return nil
	}

	return packets
}
