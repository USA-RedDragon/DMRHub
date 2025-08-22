// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/puzpuzpuz/xsync/v3"
)

func makeInMemoryKV(_ *config.Config) (KV, error) {
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

func (kv inMemoryKV) Close() error {
	// No resources to close in in-memory implementation
	return nil
}
