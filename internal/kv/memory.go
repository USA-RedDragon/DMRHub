// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package kv

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/puzpuzpuz/xsync/v4"
)

const kvStatusSuccess = "success"

func makeInMemoryKV(ctx context.Context, _ *config.Config) (KV, error) {
	ctx, cancel := context.WithCancel(ctx)
	kv := inMemoryKV{
		kv:      xsync.NewMap[string, kvValue](),
		cancel:  cancel,
		metrics: metrics.NewMetrics(),
	}

	// Start background cleanup goroutine
	go kv.cleanupExpiredKeys(ctx)

	return kv, nil
}

type kvValue struct {
	value []byte
	ttl   time.Time
}

type inMemoryKV struct {
	kv      *xsync.Map[string, kvValue]
	cancel  context.CancelFunc
	metrics *metrics.Metrics
}

func (kv inMemoryKV) Has(_ context.Context, key string) (bool, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("has", "success", duration)
	}()

	obj, ok := kv.kv.Load(key)
	if !ok {
		return false, nil
	}
	if !obj.ttl.IsZero() && obj.ttl.Before(time.Now()) {
		kv.kv.Delete(key) // Remove expired key
		return false, nil
	}
	return true, nil
}

func (kv inMemoryKV) Get(_ context.Context, key string) ([]byte, error) {
	start := time.Now()
	var status string
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("get", status, duration)
	}()

	value, ok := kv.kv.Load(key)
	if !ok {
		status = "not_found"
		return nil, fmt.Errorf("key %s not found", key)
	}
	if !value.ttl.IsZero() && value.ttl.Before(time.Now()) {
		kv.kv.Delete(key) // Remove expired key
		status = "expired"
		return nil, fmt.Errorf("key %s has expired", key)
	}
	status = kvStatusSuccess
	return value.value, nil // Return the first value
}

func (kv inMemoryKV) Set(_ context.Context, key string, value []byte) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("set", "success", duration)
	}()

	kv.kv.Store(key, kvValue{
		value: value,
	})
	return nil
}

func (kv inMemoryKV) Delete(_ context.Context, key string) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("delete", "success", duration)
	}()

	kv.kv.Delete(key)
	return nil
}

func (kv inMemoryKV) Expire(_ context.Context, key string, ttl time.Duration) error {
	start := time.Now()
	var status string
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("expire", status, duration)
	}()

	var found bool
	kv.kv.Compute(key, func(oldValue kvValue, loaded bool) (kvValue, xsync.ComputeOp) {
		if !loaded {
			found = false
			return kvValue{}, xsync.CancelOp
		}
		found = true
		if ttl <= 0 {
			return kvValue{}, xsync.DeleteOp
		}
		oldValue.ttl = time.Now().Add(ttl)
		return oldValue, xsync.UpdateOp
	})
	if !found {
		status = "not_found"
		return fmt.Errorf("key %s not found", key)
	}
	if ttl <= 0 {
		status = "deleted"
	} else {
		status = kvStatusSuccess
	}
	return nil
}

func (kv inMemoryKV) Scan(_ context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("scan", "success", duration)
	}()

	keys := make([]string, 0)
	keyCount := 0
	kv.kv.Range(func(key string, value kvValue) bool {
		keyCount++
		// Check if the key matches the pattern
		var matched bool
		if match == "" {
			matched = true // Empty pattern matches all keys
		} else {
			var err error
			matched, err = filepath.Match(match, key)
			if err != nil {
				// If pattern is invalid, fall back to exact match
				matched = (match == key)
			}
		}

		if matched {
			if !value.ttl.IsZero() && value.ttl.Before(time.Now()) {
				kv.kv.Delete(key) // Remove expired keys
				return true       // continue iteration
			}
			keys = append(keys, key)
		}
		return true // continue iteration
	})

	// Update total key count metric
	kv.metrics.SetKVKeysTotal(float64(keyCount))

	return keys, 0, nil // cursor is not used in this implementation
}

func (kv inMemoryKV) SetNX(_ context.Context, key string, value string, ttl time.Duration) (bool, error) {
	start := time.Now()
	var status string
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("setnx", status, duration)
	}()

	var set bool
	kv.kv.Compute(key, func(oldValue kvValue, loaded bool) (kvValue, xsync.ComputeOp) {
		if loaded && (oldValue.ttl.IsZero() || !oldValue.ttl.Before(time.Now())) {
			// Key exists and is not expired.
			set = false
			return oldValue, xsync.CancelOp
		}
		set = true
		nv := kvValue{value: []byte(value)}
		if ttl > 0 {
			nv.ttl = time.Now().Add(ttl)
		}
		return nv, xsync.UpdateOp
	})

	if set {
		status = kvStatusSuccess
	} else {
		status = "not_set"
	}
	return set, nil
}

func (kv inMemoryKV) RPush(_ context.Context, key string, value []byte) (int64, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("rpush", "success", duration)
	}()

	var length int64
	kv.kv.Compute(key, func(oldValue kvValue, loaded bool) (kvValue, xsync.ComputeOp) {
		var newList []byte
		if loaded && oldValue.value != nil {
			newList = append(append([]byte{}, oldValue.value...), encodeListEntry(value)...)
		} else {
			newList = encodeListEntry(value)
		}
		length = countListEntries(newList)
		return kvValue{value: newList, ttl: oldValue.ttl}, xsync.UpdateOp
	})
	return length, nil
}

func (kv inMemoryKV) LDrain(_ context.Context, key string) ([][]byte, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("ldrain", "success", duration)
	}()

	existing, loaded := kv.kv.LoadAndDelete(key)
	if !loaded || existing.value == nil {
		return nil, nil
	}
	return decodeListEntries(existing.value), nil
}

// encodeListEntry encodes a single entry as [4-byte big-endian length][data].
func encodeListEntry(data []byte) []byte {
	l := len(data)
	entry := make([]byte, 4+l)
	entry[0] = byte(l >> 24)
	entry[1] = byte(l >> 16)
	entry[2] = byte(l >> 8)
	entry[3] = byte(l)
	copy(entry[4:], data)
	return entry
}

// decodeListEntries decodes entries from the format produced by encodeListEntry.
func decodeListEntries(data []byte) [][]byte {
	var entries [][]byte
	for len(data) >= 4 {
		l := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
		data = data[4:]
		if l > len(data) {
			break
		}
		entry := make([]byte, l)
		copy(entry, data[:l])
		entries = append(entries, entry)
		data = data[l:]
	}
	return entries
}

// countListEntries counts the number of entries in the encoded list.
func countListEntries(data []byte) int64 {
	var count int64
	for len(data) >= 4 {
		l := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
		data = data[4:]
		if l > len(data) {
			break
		}
		data = data[l:]
		count++
	}
	return count
}

func (kv inMemoryKV) Close() error {
	// Cancel the cleanup goroutine
	kv.cancel()
	return nil
}

// cleanupExpiredKeys runs periodically to remove expired keys
func (kv inMemoryKV) cleanupExpiredKeys(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return // Context cancelled, stop cleanup
		case <-ticker.C:
			// Perform cleanup with metrics
			start := time.Now()
			now := time.Now()
			keysToDelete := make([]string, 0)

			// Collect expired keys
			kv.kv.Range(func(key string, value kvValue) bool {
				if !value.ttl.IsZero() && value.ttl.Before(now) {
					keysToDelete = append(keysToDelete, key)
				}
				return true // continue iteration
			})

			// Delete expired keys
			for _, key := range keysToDelete {
				kv.kv.Delete(key)
			}

			// Record metrics
			duration := time.Since(start).Seconds()
			expiredCount := float64(len(keysToDelete))
			kv.metrics.RecordKVCleanup(duration)
			kv.metrics.IncrementKVExpiredKeys(expiredCount)
		}
	}
}
