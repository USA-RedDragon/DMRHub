package dmr

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/internal/models"
	"github.com/redis/go-redis/v9"
)

type redisParrotStorage struct {
	Redis *redis.Client
}

func makeRedisParrotStorage(redis *redis.Client) redisParrotStorage {
	return redisParrotStorage{
		Redis: redis,
	}
}

func (r *redisParrotStorage) store(ctx context.Context, streamId uint, repeaterId uint) {
	r.Redis.Set(ctx, fmt.Sprintf("parrot:stream:%d", streamId), repeaterId, 5*time.Minute)
}

func (r *redisParrotStorage) exists(ctx context.Context, streamId uint) bool {
	return r.Redis.Exists(ctx, fmt.Sprintf("parrot:stream:%d", streamId)).Val() == 1
}

func (r *redisParrotStorage) refresh(ctx context.Context, streamId uint) {
	r.Redis.Expire(ctx, fmt.Sprintf("parrot:stream:%d", streamId), 5*time.Minute)
}

func (r *redisParrotStorage) get(ctx context.Context, streamId uint) (uint, error) {
	repeaterIdStr, err := r.Redis.Get(ctx, fmt.Sprintf("parrot:stream:%d", streamId)).Result()
	if err != nil {
		return 0, err
	}
	repeaterId, err := strconv.Atoi(repeaterIdStr)
	if err != nil {
		return 0, err
	}
	return uint(repeaterId), nil
}

func (r *redisParrotStorage) stream(ctx context.Context, streamId uint, packet models.Packet) error {
	packetBytes, err := packet.MarshalMsg(nil)
	if err != nil {
		return err
	}

	r.Redis.RPush(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamId), packetBytes)
	return nil
}

func (r *redisParrotStorage) delete(ctx context.Context, streamId uint) {
	r.Redis.Del(ctx, fmt.Sprintf("parrot:stream:%d", streamId))
	r.Redis.Expire(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamId), 5*time.Minute)
}

func (r *redisParrotStorage) getStream(ctx context.Context, streamId uint) ([]models.Packet, error) {
	// Empty array of packet byte arrays
	var packets [][]byte
	packetSize, err := r.Redis.LLen(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamId)).Result()
	if err != nil {
		return nil, err
	}
	// Loop through the packets and add them to the array
	for i := int64(0); i < packetSize; i++ {
		packet, err := r.Redis.LIndex(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamId), i).Bytes()
		if err != nil {
			return nil, err
		}
		packets = append(packets, packet)
	}
	// Delete the stream
	r.Redis.Del(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamId))

	// Empty array of packets
	var packetArray []models.Packet
	// Loop through the packets and unmarshal them
	for _, packet := range packets {
		var packetObj models.Packet
		_, err := packetObj.UnmarshalMsg(packet)
		if err != nil {
			return nil, err
		}
		packetArray = append(packetArray, packetObj)
	}
	return packetArray, nil
}
