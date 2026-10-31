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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordProbe is the transport of a replica in a test. It keeps what
// the replica writes and runs atAck at the third RPTACK, which ends a
// login.
type recordProbe struct {
	out   chan []byte
	acks  atomic.Int32
	atAck func()
}

func (p *recordProbe) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, net.ErrClosed }

func (p *recordProbe) Close() error { return nil }

func (p *recordProbe) WriteTo(b []byte, _ net.Addr) (int, error) {
	if dmrd.Classify(b, dmrd.DialectMMDVM) == dmrd.CommandRPTACK && p.acks.Add(1) == 3 && p.atAck != nil {
		p.atAck()
	}
	p.out <- append([]byte(nil), b...)
	return len(b), nil
}

// recordRig is two replicas that share a database, a KV store and a
// pubsub, as a rolling restart has them.
type recordRig struct {
	t    *testing.T
	kv   *servers.KVClient
	a, b *Server
	pa   *recordProbe
	pb   *recordProbe
	from endpoint
	id   uint32
	done <-chan struct{}
}

func newRecordRig(t *testing.T, id uint32) *recordRig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(id)},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              testPassword,
	}).Error)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = kvStore.Close()
	})
	replica := func() (*Server, *recordProbe) {
		h := hub.NewHub(database, kvStore, ps, nil)
		s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
		require.NoError(t, err)
		s.newSalt = func() (uint32, error) { return testSalt, nil }
		require.NoError(t, s.Start(context.Background()))
		t.Cleanup(func() {
			_ = s.Stop(context.Background())
			h.Stop(context.Background())
		})
		p := &recordProbe{out: make(chan []byte, 64)}
		s.transports.Store(probeTransport, p)
		return &s, p
	}
	r := &recordRig{t: t, kv: servers.MakeKVClient(kvStore), id: id}
	r.a, r.pa = replica()
	r.b, r.pb = replica()
	r.from = endpoint{transport: probeTransport, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	t.Cleanup(cancel)
	r.done = ctx.Done()
	return r
}

func (r *recordRig) next(p *recordProbe, what string) dmrd.Command {
	r.t.Helper()
	select {
	case b := <-p.out:
		return dmrd.Classify(b, dmrd.DialectMMDVM)
	case <-r.done:
		require.FailNow(r.t, "no "+what+" within "+answerWait.String())
		return 0
	}
}

func (r *recordRig) send(s *Server, c dmrd.Control) {
	r.t.Helper()
	p, err := c.AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(r.t, err)
	s.handleFrom(context.Background(), r.from, p, time.Now())
}

func (r *recordRig) auth() {
	r.t.Helper()
	r.send(r.a, dmrd.Control{Command: dmrd.CommandRPTL, ID: r.id})
	require.Equal(r.t, dmrd.CommandRPTACK, r.next(r.pa, "salt"))
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte(testPassword))
	require.NoError(r.t, err)
	r.send(r.a, dmrd.Control{Command: dmrd.CommandRPTK, ID: r.id, Token: token})
	require.Equal(r.t, dmrd.CommandRPTACK, r.next(r.pa, "RPTACK of the login"))
}

// stored reports the KV record of the repeater of r as replica b would
// find it.
func (r *recordRig) stored() (models.Repeater, bool) {
	rec, err := r.kv.GetRepeater(context.Background(), uint(r.id))
	return rec, err == nil
}

// TestARepeaterResumesOnAnotherReplicaFromTheAckOfItsLogin logs a
// repeater in to replica a through the path of the listener, which does
// not wait for the records queue, and at the moment a writes the RPTACK of
// the config sends a ping of the repeater to replica b, as a load balancer
// that moves the repeater in a rolling restart does. The KV record of the
// login must be there at that RPTACK, and b must resume the session.
func TestARepeaterResumesOnAnotherReplicaFromTheAckOfItsLogin(t *testing.T) {
	t.Parallel()
	r := newRecordRig(t, 311895)
	var (
		atAck models.Repeater
		found bool
	)
	r.pa.atAck = func() {
		atAck, found = r.stored()
		r.send(r.b, dmrd.Control{Command: dmrd.CommandRPTPING, ID: r.id})
	}
	r.auth()
	r.a.handleFrom(context.Background(), r.from, rptc(t, dmrd.DialectMMDVM, r.id), time.Now())
	require.Equal(t, dmrd.CommandRPTACK, r.next(r.pa, "RPTACK of the config"))

	require.True(t, found, "the KV record of the login was not there at the RPTACK of the config")
	assert.Equal(t, models.RepeaterStateConnected, atAck.Connection)
	assert.Equal(t, "MMDVM", atAck.Dialect)
	assert.True(t, r.from.matchesKV(atAck.IP, atAck.Port))
	assert.NotEmpty(t, atAck.SecretCheck)
	require.Equal(t, dmrd.CommandMSTPONG, r.next(r.pb, "answer of replica b to the ping"))
}

// TestALoginIsNotAckedWhileItsRecordCannotBeQueued logs a repeater in
// while the records queue of the repeater is full, so that the KV record
// of the login cannot be queued. The server must withhold the RPTACK of
// the config, as the repeater sends its config again, and ack it once the
// record is stored.
func TestALoginIsNotAckedWhileItsRecordCannotBeQueued(t *testing.T) {
	t.Parallel()
	r := newRecordRig(t, 311896)
	var found atomic.Bool
	r.pa.atAck = func() {
		_, ok := r.stored()
		found.Store(ok)
	}
	r.auth()

	release, running := make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	require.True(t, r.a.records.Do(uint(r.id), func() {
		close(running)
		<-release
	}))
	<-running
	for r.a.records.Do(uint(r.id), func() {}) {
	}
	r.a.handleFrom(context.Background(), r.from, rptc(t, dmrd.DialectMMDVM, r.id), time.Now())
	r.a.lookups.Wait()
	select {
	case b := <-r.pa.out:
		require.FailNow(t, "the server answered the config while the record of the login could not be queued",
			"answer %v, record stored %v", dmrd.Classify(b, dmrd.DialectMMDVM), found.Load())
	default:
	}

	free()
	r.a.records.Wait()
	r.a.handleFrom(context.Background(), r.from, rptc(t, dmrd.DialectMMDVM, r.id), time.Now())
	require.Equal(t, dmrd.CommandRPTACK, r.next(r.pa, "RPTACK of the config sent again"))
	require.True(t, found.Load(), "the KV record of the login was not there at the RPTACK of the config")
}
