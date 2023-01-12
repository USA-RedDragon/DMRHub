package dmr

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

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

func (p *Parrot) startStream(stream_id int, peer_id int) bool {
	if !p.isStarted(stream_id) {
		p.Redis.Set(fmt.Sprintf("parrot:stream:%d", stream_id), peer_id, 5*time.Minute)
		return true
	}
	klog.Warningf("Parrot: Stream %d already started", stream_id)
	return false
}

func (p *Parrot) isStarted(stream_id int) bool {
	return p.Redis.Exists(fmt.Sprintf("parrot:stream:%d", stream_id)).Val() == 1
}

func (p *Parrot) recordPacket(stream_id int, data []byte) {
	p.Redis.Expire(fmt.Sprintf("parrot:stream:%d", stream_id), 5*time.Minute)

	// Grab the peer ID to go ahead and mark the packet as being routed back
	peer_id_str, err := p.Redis.Get(fmt.Sprintf("parrot:stream:%d", stream_id)).Result()
	if err != nil {
		klog.Exitf("Error getting parrot stream from redis", err)
	}
	peer_id, err := strconv.Atoi(peer_id_str)
	if err != nil {
		klog.Exitf("Error parsing parrot stream from redis", err)
	}
	peerBinary := make([]byte, 4)
	binary.BigEndian.PutUint32(peerBinary, uint32(peer_id))
	pkt := append(data[:11], peerBinary[:4]...)
	pkt = append(pkt, data[15:]...)

	// We also need to swap the source and destination IDs in the packet
	// take (zero-indexed) bytes 5, 6, and 7 and put them in 8, 9, and 10
	// take (zero-indexed) bytes 8, 9, and 10 and put them in 5, 6, and 7
	pkt[5], pkt[6], pkt[7], pkt[8], pkt[9], pkt[10] = pkt[8], pkt[9], pkt[10], pkt[5], pkt[6], pkt[7]

	p.Redis.RPush(fmt.Sprintf("parrot:stream:%d:packets", stream_id), pkt)
}

func (p *Parrot) stopStream(stream_id int) {
	p.Redis.Del(fmt.Sprintf("parrot:stream:%d", stream_id))
	p.Redis.Expire(fmt.Sprintf("parrot:stream:%d:packets", stream_id), 5*time.Minute)
}

func (p *Parrot) getStream(stream_id int) [][]byte {
	// Empty array of packet byte arrays
	var packets [][]byte
	packetSize, err := p.Redis.LLen(fmt.Sprintf("parrot:stream:%d:packets", stream_id)).Result()
	if err != nil {
		klog.Exitf("Error getting parrot stream from redis", err)
	}
	// Loop through the packets and add them to the array
	for i := int64(0); i < packetSize; i++ {
		packet, err := p.Redis.LIndex(fmt.Sprintf("parrot:stream:%d:packets", stream_id), i).Bytes()
		if err != nil {
			klog.Exitf("Error getting parrot stream from redis", err)
		}
		packets = append(packets, packet)
	}
	// Delete the stream
	p.Redis.Del(fmt.Sprintf("parrot:stream:%d:packets", stream_id))
	return packets
}
