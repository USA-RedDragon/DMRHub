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

package transcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
)

// LeaseTTL is how long a call holds its AMBE server after its last burst,
// so that the slot of a replica that stopped comes free. A call renews its
// lease every LeaseTTL/3.
const LeaseTTL = 10 * time.Second

const leasePrefix = "dmrhub:ambe:"

// kvTimeout bounds each KV request of a lease.
const kvTimeout = time.Second

// leases counts the calls that hold one AMBE server. A server with a limit
// has that many slot keys, each held by one call with SetNX; a server
// without one has a key per call. Without a KV store the count is of this
// process only.
type leases struct {
	kv     kv.KV
	limit  int
	prefix string

	mu    sync.Mutex
	local int
}

func newLeases(store kv.KV, c ServerConfig) *leases {
	return &leases{kv: store, limit: c.Calls, prefix: leasePrefix + hex.EncodeToString([]byte(c.Addr)) + ":"}
}

// lease is one call that holds a server.
type lease struct {
	ls      *leases
	key     string
	token   string
	renewed time.Time
}

func newToken() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// take gives the call a place on the server, or reports false when the
// server holds as many calls as it takes.
func (ls *leases) take(ctx context.Context) (*lease, bool) {
	token := newToken()
	if ls.kv == nil {
		ls.mu.Lock()
		defer ls.mu.Unlock()
		if ls.limit > 0 && ls.local >= ls.limit {
			return nil, false
		}
		ls.local++
		return &lease{ls: ls, token: token, renewed: time.Now()}, true
	}
	ctx, cancel := context.WithTimeout(ctx, kvTimeout)
	defer cancel()
	if ls.limit == 0 {
		key := ls.prefix + "call:" + token
		if _, err := ls.kv.SetNX(ctx, key, token, LeaseTTL); err != nil {
			slog.Warn("Cannot count a call on an AMBE server", "error", err)
		}
		return &lease{ls: ls, key: key, token: token, renewed: time.Now()}, true
	}
	for i := range ls.limit {
		key := ls.prefix + "slot:" + strconv.Itoa(i)
		ok, err := ls.kv.SetNX(ctx, key, token, LeaseTTL)
		if err != nil {
			slog.Warn("Cannot take a slot of an AMBE server", "error", err)
			return nil, false
		}
		if ok {
			return &lease{ls: ls, key: key, token: token, renewed: time.Now()}, true
		}
	}
	return nil, false
}

// renew keeps the lease for another LeaseTTL, at most every LeaseTTL/3.
func (l *lease) renew(ctx context.Context) {
	if l.ls.kv == nil || time.Since(l.renewed) < LeaseTTL/3 {
		return
	}
	l.renewed = time.Now()
	ctx, cancel := context.WithTimeout(ctx, kvTimeout)
	defer cancel()
	if err := l.ls.kv.Expire(ctx, l.key, LeaseTTL); err != nil {
		slog.Warn("Cannot renew the hold of an AMBE server", "error", err)
	}
}

// release gives the place back, unless it already went to another call.
func (l *lease) release(ctx context.Context) {
	if l.ls.kv == nil {
		l.ls.mu.Lock()
		l.ls.local--
		l.ls.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(ctx, kvTimeout)
	defer cancel()
	v, err := l.ls.kv.Get(ctx, l.key)
	if err != nil || string(v) != l.token {
		return
	}
	if err := l.ls.kv.Delete(ctx, l.key); err != nil {
		slog.Warn("Cannot free the hold of an AMBE server", "error", err)
	}
}

// count returns the number of calls that hold the server.
func (ls *leases) count(ctx context.Context) int {
	if ls.kv == nil {
		ls.mu.Lock()
		defer ls.mu.Unlock()
		return ls.local
	}
	ctx, cancel := context.WithTimeout(ctx, kvTimeout)
	defer cancel()
	n := 0
	var cursor uint64
	for {
		keys, next, err := ls.kv.Scan(ctx, cursor, ls.prefix+"*", 100)
		if err != nil {
			return n
		}
		n += len(keys)
		if next == 0 {
			return n
		}
		cursor = next
	}
}
