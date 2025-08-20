package kv

import (
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/puzpuzpuz/xsync/v3"
)

func makeInMemoryKV(config *config.Config) (KV, error) {
	return inMemoryKV{
		kv: xsync.NewMapOf[string, kvValue](),
	}, nil
}

type kvValue struct {
	values [][]byte
	ttl    time.Time
}

type inMemoryKV struct {
	kv *xsync.MapOf[string, kvValue]
}

func (kv inMemoryKV) Has(key string) (bool, error) {
	obj, ok := kv.kv.Load(key)
	if obj.ttl.Before(time.Now()) {
		kv.kv.Delete(key) // Remove expired key
		return false, nil
	}
	return ok, nil
}

func (kv inMemoryKV) Get(key string) ([]byte, error) {
	value, ok := kv.kv.Load(key)
	if !ok {
		return nil, fmt.Errorf("key %s not found", key)
	}
	if len(value.values) == 0 {
		return nil, fmt.Errorf("key %s has no values", key)
	}
	if value.ttl.Before(time.Now()) {
		kv.kv.Delete(key) // Remove expired key
		return nil, fmt.Errorf("key %s has expired", key)
	}
	return value.values[0], nil // Return the first value
}

func (kv inMemoryKV) Set(key string, value []byte) error {
	kv.kv.Store(key, kvValue{
		values: [][]byte{value},
	})
	return nil
}

func (kv inMemoryKV) Delete(key string) error {
	kv.kv.Delete(key)
	return nil
}

func (kv inMemoryKV) Expire(key string, ttl time.Duration) error {
	value, ok := kv.kv.Load(key)
	if !ok {
		return fmt.Errorf("key %s not found", key)
	}
	if ttl <= 0 {
		kv.kv.Delete(key) // Remove the key if ttl is zero or negative
		return nil
	}
	value.ttl = time.Now().Add(ttl)
	kv.kv.Store(key, value)
	return nil
}

func (kv inMemoryKV) Scan(cursor uint64, match string, count int64) ([]string, uint64, error) {
	keys := make([]string, 0)
	kv.kv.Range(func(key string, value kvValue) bool {
		if match == "" || match == key {
			if value.ttl.Before(time.Now()) {
				kv.kv.Delete(key) // Remove expired keys
				return true       // continue iteration
			}
			keys = append(keys, key)
		}
		return true // continue iteration
	})
	return keys, 0, nil // cursor is not used in this implementation
}

func (kv inMemoryKV) LLen(key string) (int64, error) {
	value, ok := kv.kv.Load(key)
	if !ok {
		return 0, fmt.Errorf("key %s not found", key)
	}
	if value.ttl.Before(time.Now()) {
		kv.kv.Delete(key) // Remove expired key
		return 0, fmt.Errorf("key %s has expired", key)
	}
	return int64(len(value.values)), nil
}

func (kv inMemoryKV) LIndex(key string, index int64) ([]byte, error) {
	value, ok := kv.kv.Load(key)
	if !ok || index < 0 || int(index) >= len(value.values) {
		return nil, nil // Key does not exist or index out of range
	}
	if value.ttl.Before(time.Now()) {
		kv.kv.Delete(key) // Remove expired key
		return nil, fmt.Errorf("key %s has expired", key)
	}
	return value.values[index], nil // Return the value at the specified index
}

func (kv inMemoryKV) RPush(key string, values ...[]byte) (int64, error) {
	currentValue, ok := kv.kv.Load(key)
	if !ok {
		currentValue = kvValue{values: make([][]byte, 0)}
	}

	// Append new values to the existing values
	currentValue.values = append(currentValue.values, values...)
	kv.kv.Store(key, currentValue)

	return int64(len(currentValue.values)), nil // Return the new length of the list
}

func (kv inMemoryKV) Close() error {
	// No resources to close in in-memory implementation
	return nil
}
