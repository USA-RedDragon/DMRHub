package dmr

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"
)

type redisRepeaterStorage struct {
	Redis *redis.Client
}

func makeRedisRepeaterStorage(redis *redis.Client) redisRepeaterStorage {
	return redisRepeaterStorage{
		Redis: redis,
	}
}

func (s *redisRepeaterStorage) ping(ctx context.Context, repeaterID uint) {
	repeater, err := s.get(ctx, repeaterID)
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return
	}
	repeater.LastPing = time.Now()
	s.store(ctx, repeaterID, repeater)
	s.Redis.Expire(ctx, fmt.Sprintf("repeater:%d", repeaterID), 5*time.Minute)
}

func (s *redisRepeaterStorage) updateConnection(ctx context.Context, repeaterID uint, connection string) {
	repeater, err := s.get(ctx, repeaterID)
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return
	}
	repeater.Connection = connection
	s.store(ctx, repeaterID, repeater)
}

func (s *redisRepeaterStorage) delete(ctx context.Context, repeaterId uint) bool {
	return s.Redis.Del(ctx, fmt.Sprintf("repeater:%d", repeaterId)).Val() == 1
}

func (s *redisRepeaterStorage) store(ctx context.Context, repeaterId uint, repeater models.Repeater) {
	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		klog.Errorf("Error marshalling repeater", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	s.Redis.Set(ctx, fmt.Sprintf("repeater:%d", repeaterId), repeaterBytes, 5*time.Minute)
}

func (s *redisRepeaterStorage) get(ctx context.Context, repeaterId uint) (models.Repeater, error) {
	repeaterBits, err := s.Redis.Get(ctx, fmt.Sprintf("repeater:%d", repeaterId)).Result()
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return models.Repeater{}, err
	}
	var repeater models.Repeater
	_, err = repeater.UnmarshalMsg([]byte(repeaterBits))
	if err != nil {
		klog.Errorf("Error unmarshalling repeater", err)
		return models.Repeater{}, err
	}
	return repeater, nil
}

func (s *redisRepeaterStorage) exists(ctx context.Context, repeaterId uint) bool {
	return s.Redis.Exists(ctx, fmt.Sprintf("repeater:%d", repeaterId)).Val() == 1
}

func (s *redisRepeaterStorage) list(ctx context.Context) ([]uint, error) {
	var cursor uint64
	var repeaters []uint
	for {
		keys, cursor, err := s.Redis.Scan(ctx, cursor, "repeater:*", 0).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			repeaterNum, err := strconv.Atoi(strings.Replace(key, "repeater:", "", 1))
			if err != nil {
				return nil, err
			}
			repeaters = append(repeaters, uint(repeaterNum))
		}

		if cursor == 0 {
			break
		}
	}
	return repeaters, nil
}
