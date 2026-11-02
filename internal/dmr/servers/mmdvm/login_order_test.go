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
	"strconv"
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

const (
	probeTransport = "probe"
	// answerWait bounds each wait of the test for the server, so that a
	// missing answer fails the test with what was missing. A repeater
	// gives up its login after dmrdsession.LoginTimeoutDefault, so an
	// answer that takes longer has failed.
	answerWait = dmrdsession.LoginTimeoutDefault
)

// loginProbe is a transport that keeps what the server writes, and runs
// atAck when the server writes the RPTACK that ends a login.
type loginProbe struct {
	out   chan []byte
	acks  atomic.Int32
	atAck func()
}

func (p *loginProbe) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, net.ErrClosed }

func (p *loginProbe) Close() error { return nil }

func (p *loginProbe) WriteTo(b []byte, _ net.Addr) (int, error) {
	if dmrd.Classify(b, dmrd.DialectMMDVM) == dmrd.CommandRPTACK {
		if p.acks.Add(1) == 3 {
			p.atAck()
		}
	}
	p.out <- append([]byte(nil), b...)
	return len(b), nil
}

// TestARepeaterGetsItsCallsFromTheAckOfItsLogin logs a repeater in and,
// at the moment the server writes the RPTACK of its config, publishes a
// private call to it. A repeater that is told that its login passed
// expects every call from then on, so the server must subscribe it to its
// calls before that RPTACK.
func TestARepeaterGetsItsCallsFromTheAckOfItsLogin(t *testing.T) {
	t.Parallel()
	const id, stream = uint32(311890), uint(76101)
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
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
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
		Dst:         uint(id),
		Repeater:    100001,
		FrameType:   dmrconst.FrameVoice,
		DTypeOrVSeq: dmrconst.VoiceA,
		StreamID:    stream,
		BER:         -1,
		RSSI:        -1,
	}
	msg, err := (&models.RawDMRPacket{Data: call.Encode()}).MarshalMsg(nil)
	require.NoError(t, err)
	probe := &loginProbe{out: make(chan []byte, 16)}
	published := make(chan error, 1)
	probe.atAck = func() {
		published <- ps.Publish("hub:packets:repeater:"+strconv.FormatUint(uint64(id), 10), msg)
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
	require.NoError(t, <-published)

	for {
		p := next("call that was published at the RPTACK of the config")
		if dmrd.Classify(p, dmrd.DialectMMDVM) != dmrd.CommandDMRD {
			continue
		}
		got, ok := models.UnpackPacket(p)
		require.True(t, ok)
		require.Equal(t, stream, got.StreamID)
		return
	}
}
