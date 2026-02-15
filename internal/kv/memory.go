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

func (kv inMemoryKV) Has(key string) (bool, error) {
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

func (kv inMemoryKV) Get(key string) ([]byte, error) {
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
	status = "success"
	return value.value, nil // Return the first value
}

func (kv inMemoryKV) Set(key string, value []byte) error {
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

func (kv inMemoryKV) Delete(key string) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("delete", "success", duration)
	}()

	kv.kv.Delete(key)
	return nil
}

func (kv inMemoryKV) Expire(key string, ttl time.Duration) error {
	start := time.Now()
	var status string
	defer func() {
		duration := time.Since(start).Seconds()
		kv.metrics.RecordKVOperation("expire", status, duration)
	}()

	value, ok := kv.kv.Load(key)
	if !ok {
		status = "not_found"
		return fmt.Errorf("key %s not found", key)
	}
	if ttl <= 0 {
		kv.kv.Delete(key) // Remove the key if ttl is zero or negative
		status = "deleted"
		return nil
	}
	value.ttl = time.Now().Add(ttl)
	kv.kv.Store(key, value)
	status = "success"
	return nil
}

func (kv inMemoryKV) Scan(cursor uint64, match string, count int64) ([]string, uint64, error) {
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
