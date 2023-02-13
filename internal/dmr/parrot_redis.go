package dmr

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
)

type redisParrotStorage struct {
	Redis *redis.Client
}

var (
	ErrRedis        = fmt.Errorf("redis error")
	ErrCast         = fmt.Errorf("cast error")
	ErrMarshal      = fmt.Errorf("marshal error")
	ErrUnmarshal    = fmt.Errorf("unmarshal error")
	ErrNoSuchStream = fmt.Errorf("no such stream")
)

const parrotExpireTime = 5 * time.Minute

func makeRedisParrotStorage(redis *redis.Client) redisParrotStorage {
	return redisParrotStorage{
		Redis: redis,
	}
}

func (r *redisParrotStorage) store(ctx context.Context, streamID uint, repeaterID uint) {
	r.Redis.Set(ctx, fmt.Sprintf("parrot:stream:%d", streamID), repeaterID, parrotExpireTime)
}

func (r *redisParrotStorage) exists(ctx context.Context, streamID uint) bool {
	return r.Redis.Exists(ctx, fmt.Sprintf("parrot:stream:%d", streamID)).Val() == 1
}

func (r *redisParrotStorage) refresh(ctx context.Context, streamID uint) {
	r.Redis.Expire(ctx, fmt.Sprintf("parrot:stream:%d", streamID), parrotExpireTime)
}

func (r *redisParrotStorage) get(ctx context.Context, streamID uint) (uint, error) {
	repeaterIDStr, err := r.Redis.Get(ctx, fmt.Sprintf("parrot:stream:%d", streamID)).Result()
	if err != nil {
		return 0, ErrRedis
	}
	repeaterID, err := strconv.Atoi(repeaterIDStr)
	if err != nil {
		return 0, ErrCast
	}
	return uint(repeaterID), nil
}

func (r *redisParrotStorage) stream(ctx context.Context, streamID uint, packet models.Packet) error {
	packetBytes, err := packet.MarshalMsg(nil)
	if err != nil {
		return ErrMarshal
	}

	r.Redis.RPush(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID), packetBytes)
	return nil
}

func (r *redisParrotStorage) delete(ctx context.Context, streamID uint) {
	r.Redis.Del(ctx, fmt.Sprintf("parrot:stream:%d", streamID))
	r.Redis.Expire(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID), parrotExpireTime)
}

func (r *redisParrotStorage) getStream(ctx context.Context, streamID uint) ([]models.Packet, error) {
	// Empty array of packet byte arrays
	var packets [][]byte
	packetSize, err := r.Redis.LLen(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID)).Result()
	if err != nil {
		return nil, ErrNoSuchStream
	}
	// Loop through the packets and add them to the array
	for i := int64(0); i < packetSize; i++ {
		packet, err := r.Redis.LIndex(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID), i).Bytes()
		if err != nil {
			return nil, ErrNoSuchStream
		}
		packets = append(packets, packet)
	}
	// Delete the stream
	r.Redis.Del(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID))

	// Empty array of packets
	packetArray := make([]models.Packet, packetSize)
	// Loop through the packets and unmarshal them
	for _, packet := range packets {
		var packetObj models.Packet
		_, err := packetObj.UnmarshalMsg(packet)
		if err != nil {
			return nil, ErrUnmarshal
		}
		packetArray = append(packetArray, packetObj)
	}
	return packetArray, nil
}
