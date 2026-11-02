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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/require"
)

// optionsProbe is a transport that keeps what the server writes, and runs
// atAck when the server writes the RPTACK of the RPTO that follows a login.
type optionsProbe struct {
	out   chan []byte
	acks  atomic.Int32
	atAck func()
}

func (p *optionsProbe) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, net.ErrClosed }

func (p *optionsProbe) Close() error { return nil }

func (p *optionsProbe) WriteTo(b []byte, _ net.Addr) (int, error) {
	if dmrd.Classify(b, dmrd.DialectMMDVM) == dmrd.CommandRPTACK {
		if p.acks.Add(1) == 4 {
			p.atAck()
		}
	}
	p.out <- append([]byte(nil), b...)
	return len(b), nil
}

// TestARepeaterGetsTheCallsOfItsNewStaticsFromTheAckOfItsOptions logs a
// repeater in, sends an RPTO that sets a static talkgroup and, at the
// moment the server writes the RPTACK of the options, publishes a group
// call on that talkgroup. A repeater that is told that its options were
// taken expects the calls of its new static talkgroups from then on, so the
// server must subscribe it to them before that RPTACK.
func TestARepeaterGetsTheCallsOfItsNewStaticsFromTheAckOfItsOptions(t *testing.T) {
	t.Parallel()
	const id, tg, stream = uint32(990392), uint(91), uint(76102)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(id)},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              testPassword,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: "TG91"}).Error)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	s.newSalt = func() (uint32, error) { return testSalt, nil }
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() {
		_ = s.Stop(context.Background())
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})

	call := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         1000001,
		Dst:         tg,
		Repeater:    100001,
		GroupCall:   true,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    stream,
		BER:         -1,
		RSSI:        -1,
	}
	msg, err := (&models.RawDMRPacket{Data: call.Encode()}).MarshalMsg(nil)
	require.NoError(t, err)
	probe := &optionsProbe{out: make(chan []byte, 16)}
	published := make(chan error, 1)
	probe.atAck = func() {
		published <- ps.Publish("hub:packets:talkgroup:91", msg)
	}
	s.transports.Store(probeTransport, probe)
	from := endpoint{transport: probeTransport, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}

	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	next := func(what string) []byte {
		t.Helper()
		select {
		case p := <-probe.out:
			return p
		case <-ctx.Done():
			require.FailNow(t, "no "+what+" within "+answerWait.String())
			return nil
		}
	}
	send := func(c dmrd.Control) {
		p, err := c.AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		s.handleFrom(context.Background(), from, p, time.Now())
	}
	send(dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("salt"), dmrd.DialectMMDVM))
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte(testPassword))
	require.NoError(t, err)
	send(dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the login"), dmrd.DialectMMDVM))
	s.handleFrom(context.Background(), from, rptc(t, dmrd.DialectMMDVM, id), time.Now())
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the config"), dmrd.DialectMMDVM))
	send(dmrd.Control{Command: dmrd.CommandRPTO, ID: id, Text: []byte("TS1=91;")})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the options"), dmrd.DialectMMDVM))
	require.NoError(t, <-published)

	for {
		p := next("call that was published at the RPTACK of the options")
		if dmrd.Classify(p, dmrd.DialectMMDVM) != dmrd.CommandDMRD {
			continue
		}
		got, ok := models.UnpackPacket(p)
		require.True(t, ok)
		require.Equal(t, stream, got.StreamID)
		return
	}
}

// TestAnOptionsAckDoesNotReachALaterLogin holds the records queue of a
// repeater while it sends an RPTO and then logs in again, as DMRGateway
// does when its options wait times out. The RPTACK of the options must not
// go out once the session it answers has ended, or the new login takes it
// as the answer to its RPTK.
func TestAnOptionsAckDoesNotReachALaterLogin(t *testing.T) {
	t.Parallel()
	const id = uint32(990393)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(id)},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              testPassword,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: 91, Name: "TG91"}).Error)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	s.newSalt = func() (uint32, error) { return testSalt, nil }
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() {
		_ = s.Stop(context.Background())
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})

	probe := &optionsProbe{out: make(chan []byte, 16), atAck: func() {}}
	s.transports.Store(probeTransport, probe)
	from := endpoint{transport: probeTransport, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	next := func(what string) []byte {
		t.Helper()
		select {
		case p := <-probe.out:
			return p
		case <-ctx.Done():
			require.FailNow(t, "no "+what+" within "+answerWait.String())
			return nil
		}
	}
	send := func(c dmrd.Control) {
		p, err := c.AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		s.handleFrom(context.Background(), from, p, time.Now())
	}
	send(dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("salt"), dmrd.DialectMMDVM))
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte(testPassword))
	require.NoError(t, err)
	send(dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the login"), dmrd.DialectMMDVM))
	s.handleFrom(context.Background(), from, rptc(t, dmrd.DialectMMDVM, id), time.Now())
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the config"), dmrd.DialectMMDVM))
	s.records.Wait()

	gate := make(chan struct{})
	held := make(chan struct{})
	require.True(t, s.records.Do(uint(id), func() {
		close(held)
		<-gate
	}))
	<-held
	send(dmrd.Control{Command: dmrd.CommandRPTO, ID: id, Text: []byte("TS1=91;")})
	send(dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("salt of the new login"), dmrd.DialectMMDVM))
	close(gate)
	s.records.Wait()
	s.lookups.Wait()

	select {
	case p := <-probe.out:
		require.Failf(t, "a packet went out after the new login", "%q", p)
	default:
	}
	s.mu.Lock()
	state := s.links[from.String()].m.State()
	s.mu.Unlock()
	require.Equal(t, dmrdsession.StateAuth, state)
}

// TestFullRecordsQueueWithholdsTheOptionsAck sends an RPTO while the
// records queue of the repeater is full. The options cannot be applied,
// so the server must not acknowledge them: the repeater sends the RPTO
// again until it gets an RPTACK, and the next one, once the queue drains,
// must be applied and acknowledged.
func TestFullRecordsQueueWithholdsTheOptionsAck(t *testing.T) {
	t.Parallel()
	const id = uint32(990394)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(id)},
		Type:                  models.RepeaterTypeMMDVM,
		Password:              testPassword,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: 91, Name: "TG91"}).Error)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)
	s, err := MakeServer(&cfg, h, database, ps, kvStore, "test", "test")
	require.NoError(t, err)
	s.newSalt = func() (uint32, error) { return testSalt, nil }
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() {
		_ = s.Stop(context.Background())
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})

	probe := &optionsProbe{out: make(chan []byte, 16), atAck: func() {}}
	s.transports.Store(probeTransport, probe)
	from := endpoint{transport: probeTransport, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), answerWait)
	defer cancel()
	next := func(what string) []byte {
		t.Helper()
		select {
		case p := <-probe.out:
			return p
		case <-ctx.Done():
			require.FailNow(t, "no "+what+" within "+answerWait.String())
			return nil
		}
	}
	send := func(c dmrd.Control) {
		p, err := c.AppendBinary(nil, dmrd.DialectMMDVM)
		require.NoError(t, err)
		s.handleFrom(context.Background(), from, p, time.Now())
	}
	send(dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("salt"), dmrd.DialectMMDVM))
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, testSalt, []byte(testPassword))
	require.NoError(t, err)
	send(dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the login"), dmrd.DialectMMDVM))
	s.handleFrom(context.Background(), from, rptc(t, dmrd.DialectMMDVM, id), time.Now())
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the config"), dmrd.DialectMMDVM))
	s.records.Wait()

	gate, running := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	require.True(t, s.records.Do(uint(id), func() {
		close(running)
		<-gate
	}))
	<-running
	for s.records.Do(uint(id), func() { <-gate }) {
	}
	send(dmrd.Control{Command: dmrd.CommandRPTO, ID: id, Text: []byte("TS1=91;")})
	select {
	case p := <-probe.out:
		require.Failf(t, "the options of a full queue were acknowledged", "%q", p)
	default:
	}
	release()
	s.records.Wait()
	select {
	case p := <-probe.out:
		require.Failf(t, "a packet went out for options that were dropped", "%q", p)
	default:
	}

	send(dmrd.Control{Command: dmrd.CommandRPTO, ID: id, Text: []byte("TS1=91;")})
	require.Equal(t, dmrd.CommandRPTACK, dmrd.Classify(next("RPTACK of the resent options"), dmrd.DialectMMDVM))
	rpt, err := models.FindRepeaterByID(database, uint(id))
	require.NoError(t, err)
	require.Len(t, rpt.TS1StaticTalkgroups, 1)
	require.Equal(t, uint(91), rpt.TS1StaticTalkgroups[0].ID)
}
