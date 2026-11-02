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

package hub

import (
	"context"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"gorm.io/gorm"
)

// stoppingKV is the KV store of a hub, whose calls also end when the hub
// stops, so that Stop never waits on a store that does not answer.
type stoppingKV struct {
	kv   kv.KV
	stop context.Context //nolint:containedctx // the stop of the hub, which each call joins
}

func (k stoppingKV) join(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	unhook := context.AfterFunc(k.stop, cancel)
	return ctx, func() {
		unhook()
		cancel()
	}
}

func (k stoppingKV) Has(ctx context.Context, key string) (bool, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Has(ctx, key) //nolint:wrapcheck
}

func (k stoppingKV) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Get(ctx, key) //nolint:wrapcheck
}

func (k stoppingKV) Set(ctx context.Context, key string, value []byte) error {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Set(ctx, key, value) //nolint:wrapcheck
}

func (k stoppingKV) Delete(ctx context.Context, key string) error {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Delete(ctx, key) //nolint:wrapcheck
}

func (k stoppingKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Expire(ctx, key, ttl) //nolint:wrapcheck
}

func (k stoppingKV) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Scan(ctx, cursor, match, count) //nolint:wrapcheck
}

func (k stoppingKV) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

func (k stoppingKV) RPush(ctx context.Context, key string, value []byte) (int64, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.RPush(ctx, key, value) //nolint:wrapcheck
}

func (k stoppingKV) LDrain(ctx context.Context, key string) ([][]byte, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.LDrain(ctx, key) //nolint:wrapcheck
}

func (k stoppingKV) RPushTrim(ctx context.Context, key string, value []byte, keep int64, ttl time.Duration) error {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.RPushTrim(ctx, key, value, keep, ttl) //nolint:wrapcheck
}

func (k stoppingKV) LRange(ctx context.Context, key string) ([][]byte, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.LRange(ctx, key) //nolint:wrapcheck
}

func (k stoppingKV) DeleteIfEqual(ctx context.Context, key string, value []byte) (bool, error) {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.DeleteIfEqual(ctx, key, value) //nolint:wrapcheck
}

func (k stoppingKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	ctx, done := k.join(ctx)
	defer done()
	return k.kv.Update(ctx, key, ttl, fn) //nolint:wrapcheck
}

func (k stoppingKV) Close() error {
	return k.kv.Close() //nolint:wrapcheck
}

func stoppingStore(store kv.KV, stop context.Context) kv.KV {
	if store == nil {
		return nil
	}
	return stoppingKV{kv: store, stop: stop}
}

func stoppingDB(db *gorm.DB, stop context.Context) *gorm.DB {
	if db == nil {
		return nil
	}
	return db.WithContext(stop)
}
