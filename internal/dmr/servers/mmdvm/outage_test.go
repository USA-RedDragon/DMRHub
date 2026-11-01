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

package mmdvm

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errPausedRedis = errors.New("read tcp: i/o timeout")

// pausedKV acts as a Redis that stops answering while paused: each call
// fails, as a call to a paused Redis times out.
type pausedKV struct {
	kv.KV
	paused atomic.Bool
}

func (p *pausedKV) Get(ctx context.Context, key string) ([]byte, error) {
	if p.paused.Load() {
		return nil, errPausedRedis
	}
	return p.KV.Get(ctx, key) //nolint:wrapcheck
}

func (p *pausedKV) Set(ctx context.Context, key string, value []byte) error {
	if p.paused.Load() {
		return errPausedRedis
	}
	return p.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func (p *pausedKV) Delete(ctx context.Context, key string) error {
	if p.paused.Load() {
		return errPausedRedis
	}
	return p.KV.Delete(ctx, key) //nolint:wrapcheck
}

func (p *pausedKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if p.paused.Load() {
		return errPausedRedis
	}
	return p.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

func newOutageRig(t *testing.T, id uint) (*dialectRig, *pausedKV) {
	t.Helper()
	var paused *pausedKV
	r := newRig(t, []uint{id}, withKV(func(store kv.KV) kv.KV {
		paused = &pausedKV{KV: store}
		return paused
	}))
	return r, paused
}

// timeOut lets the session of the rig's repeater end for want of pings, as
// that of a hotspot that went quiet for a while does.
func (r *dialectRig) timeOut() {
	r.t.Helper()
	r.s.mu.Lock()
	r.s.epoch = r.s.epoch.Add(-dmrdsession.TimeoutDefault - time.Second)
	r.s.mu.Unlock()
	r.s.tick(context.Background())
	r.handled(r.s)
	r.quiet(r.s)
	l, _, _ := r.link(r.s)
	require.Nil(r.t, l, "the session did not end")
}

func (r *dialectRig) ping(id uint32) {
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTPING, ID: id})
}

// TestAHotspotResumesFromTheReplicasRecordInARedisOutage lets the session of
// a hotspot time out on this replica while Redis is paused, so that its
// record could be neither read nor deleted. The hotspot then pings again
// from the address that the replica remembers, and the replica must take
// the session back from its own record, for that ping and for egress. A
// ping from any other address must still wait for Redis.
func TestAHotspotResumesFromTheReplicasRecordInARedisOutage(t *testing.T) {
	t.Parallel()
	const id = 3000101
	r, paused := newOutageRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	paused.paused.Store(true)
	r.timeOut()

	r.ping(id)
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTPONG)
	l, st, _ := r.link(r.s)
	require.NotNil(t, l)
	assert.Equal(t, dmrdsession.StateRunning, st)

	r.timeOut()
	addr, ok := r.s.resumeForEgress(context.Background(), id)
	require.True(t, ok, "egress did not resume the session from the replica's record")
	assert.Equal(t, r.from.String(), addr.String())

	r.timeOut()
	other, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	otherAddr := other.LocalAddr().(*net.UDPAddr) //nolint:forcetypeassert
	r.s.handleFrom(context.Background(), udpEndpoint(*otherAddr), p, r.s.clock.Now())
	r.handled(r.s)
	marker := []byte("marker")
	_, err = r.s.Server.WriteToUDP(marker, otherAddr)
	require.NoError(t, err)
	require.NoError(t, other.SetReadDeadline(time.Now().Add(answerWait)))
	buf := make([]byte, dmrdsession.MaxPacket)
	for {
		n, _, err := other.ReadFromUDP(buf)
		require.NoError(t, err)
		if bytes.Equal(buf[:n], marker) {
			break
		}
		k, err := dmrd.DecodeControl(buf[:n], dmrd.DialectMMDVM)
		require.NoError(t, err)
		assert.NotEqual(t, dmrd.CommandMSTPONG, k.Command, "a ping from another address resumed the session")
	}
	assert.False(t, serves(r.s, id), "a ping from another address resumed the session")
}

// TestAHotspotThatEndedItsSessionDoesNotResumeInARedisOutage ends the
// session of a hotspot while Redis is up, which deletes its record, and
// then pauses Redis. The replica must not take the ended session back from
// what it remembers.
func TestAHotspotThatEndedItsSessionDoesNotResumeInARedisOutage(t *testing.T) {
	t.Parallel()
	const id = 3000102
	r, paused := newOutageRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)
	r.timeOut()
	paused.paused.Store(true)

	r.ping(id)
	r.noPong(r.s, id, "the ended session resumed")
	_, ok := r.s.resumeForEgress(context.Background(), id)
	assert.False(t, ok, "egress resumed the ended session")
}

// heldDeleteKV holds each Delete of a repeater record while held is set
// until release closes, as a slow Redis holds the delete of the record of
// an ended session.
type heldDeleteKV struct {
	kv.KV
	held    atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (h *heldDeleteKV) Delete(ctx context.Context, key string) error {
	if strings.HasPrefix(key, repeaterRecordPrefix) && h.held.CompareAndSwap(true, false) {
		close(h.entered)
		<-h.release
	}
	return h.KV.Delete(ctx, key) //nolint:wrapcheck
}

// TestAnEndedSessionDoesNotResumeFromTheRecordThatItsEndDeletes ends the
// session of a hotspot, as the server does when it ends one, while the delete
// of its KV record waits on a slow Redis. A ping that comes in then must
// not take the session back from that record.
func TestAnEndedSessionDoesNotResumeFromTheRecordThatItsEndDeletes(t *testing.T) {
	t.Parallel()
	const id = 3000103
	var held *heldDeleteKV
	r := newRig(t, []uint{id}, withKV(func(store kv.KV) kv.KV {
		held = &heldDeleteKV{KV: store, entered: make(chan struct{}), release: make(chan struct{})}
		return held
	}))
	r.login(dmrd.DialectMMDVM, id)
	r.handled(r.s)

	held.held.Store(true)
	r.s.mu.Lock()
	l := r.s.links[r.from.String()]
	now := r.s.now()
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	r.s.run(context.Background(), now, l, l.m.Close(now, buf[:0]))
	r.s.forget(l)
	r.s.mu.Unlock()
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTCL)
	<-held.entered

	var once sync.Once
	release := func() { once.Do(func() { close(held.release) }) }
	timer := time.AfterFunc(300*time.Millisecond, release)
	t.Cleanup(func() { timer.Stop() })
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	r.send(p)
	go func() {
		r.s.lookups.Wait()
		release()
	}()
	r.noPong(r.s, id, "the ended session resumed")
}
