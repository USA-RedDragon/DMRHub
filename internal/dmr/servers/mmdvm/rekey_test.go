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
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldGetKV holds the next Get after hold is set until release closes.
type heldGetKV struct {
	kv.KV
	hold    atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (h *heldGetKV) Get(ctx context.Context, key string) ([]byte, error) {
	if h.hold.CompareAndSwap(true, false) {
		close(h.entered)
		<-h.release
	}
	return h.KV.Get(ctx, key) //nolint:wrapcheck
}

// newSharedKVRig returns a rig whose KV store another replica can share.
func newSharedKVRig(t *testing.T, id uint) (*dialectRig, kv.KV) {
	t.Helper()
	r := newDialectRig(t, id)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	r.kv = servers.MakeKVClient(store)
	r.s.kvClient = r.kv
	return r, store
}

// secondReplica returns another replica that shares the database and the KV
// store of the rig, on its own socket, with the KV store under store.
func (r *dialectRig) secondReplica(store kv.KV) *Server {
	r.t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(r.t, err)
	r.t.Cleanup(func() { _ = conn.Close() })
	return &Server{
		DB:         r.s.DB,
		Server:     conn,
		kvClient:   servers.MakeKVClient(store),
		connected:  xsync.NewMap[uint, endpoint](),
		egressMiss: xsync.NewMap[uint, time.Time](),
		newSalt:    func() (uint32, error) { return testSalt, nil },
	}
}

func (r *dialectRig) noPong(s *Server, id uint32) {
	r.t.Helper()
	if p := r.read(200 * time.Millisecond); p != nil {
		k, err := dmrd.DecodeControl(p, dmrd.DialectMMDVM)
		require.NoError(r.t, err)
		assert.NotEqual(r.t, dmrd.CommandMSTPONG, k.Command, "a session that logged in with the old password answered a ping")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	assert.NotContains(r.t, s.byID, id, "a session that logged in with the old password resumed")
}

// TestAReplicaDoesNotResumeASessionThatLoggedInWithAnOldPassword logs a
// hotspot in to one replica and then changes its password while the KV
// record of that session is still there. Another replica must not resume
// the session from the record, for a ping or for egress.
func TestAReplicaDoesNotResumeASessionThatLoggedInWithAnOldPassword(t *testing.T) {
	t.Parallel()
	const id = 3000201
	r, store := newSharedKVRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.s.records.Wait()
	second := r.secondReplica(store)

	require.NoError(t, models.SetRepeaterPassword(r.s.DB, id, "n3wPassw0rd9"))
	second.endSessions(context.Background(), id)

	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	second.handlePacket(context.Background(), r.from, p)
	r.noPong(second, id)
	_, ok := second.resumeForEgress(context.Background(), id)
	assert.False(t, ok, "egress resumed a session that logged in with the old password")
}

// TestAResumeThatReadTheRecordBeforeAPasswordChangeDoesNotTakeTheSession
// holds the read of the KV record by a ping to another replica until the
// password of the hotspot changes there, as when the replica read the
// record and the password before the change was saved.
func TestAResumeThatReadTheRecordBeforeAPasswordChangeDoesNotTakeTheSession(t *testing.T) {
	t.Parallel()
	const id = 3000202
	r, store := newSharedKVRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	r.s.records.Wait()
	held := &heldGetKV{KV: store, entered: make(chan struct{}), release: make(chan struct{})}
	second := r.secondReplica(held)

	held.hold.Store(true)
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTPING, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	second.handleFrom(context.Background(), udpEndpoint(r.from), p, time.Now())
	<-held.entered
	second.endSessions(context.Background(), id)
	close(held.release)
	second.lookups.Wait()
	second.records.Wait()
	r.noPong(second, id)
}
