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

package ipsc

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
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/stretchr/testify/require"
)

// registerWait bounds each wait of the test for the server, so that a
// missing packet fails the test with what was missing.
const registerWait = 10 * time.Second

// registerProbe keeps what the server writes, and runs atReply when the
// server writes its first registration reply.
type registerProbe struct {
	out     chan []byte
	replies atomic.Int32
	atReply func()
}

func (p *registerProbe) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) {
	if len(b) > 0 && ipscwire.Opcode(b[0]) == ipscwire.OpRegisterReply && p.replies.Add(1) == 1 {
		p.atReply()
	}
	p.out <- append([]byte(nil), b...)
	return len(b), nil
}

// TestAPeerGetsItsCallsFromTheReplyToItsRegistration registers a peer
// and, at the moment the server writes the registration reply, publishes
// a private call to it. A peer that is told that it registered expects
// every call from then on, so the server must subscribe it to its calls
// before that reply.
func TestAPeerGetsItsCallsFromTheReplyToItsRegistration(t *testing.T) {
	t.Parallel()
	const id, stream = uint32(311891), uint(76102)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	cfg.DMR.IPSC.Bind = "127.0.0.1"
	cfg.DMR.IPSC.Port = 0
	cfg.DMR.IPSC.NetworkID = 311860
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(id)},
		Type:                  models.RepeaterTypeIPSC,
	}).Error)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, nil)

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
	probe := &registerProbe{out: make(chan []byte, 64)}
	published := make(chan error, 1)
	probe.atReply = func() {
		published <- ps.Publish("hub:packets:repeater:"+strconv.FormatUint(uint64(id), 10), msg)
	}

	s := NewIPSCServer(&cfg, h, database)
	s.writer = probe
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() {
		_ = s.Stop(context.Background())
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), registerWait)
	defer cancel()
	next := func(what string) []byte {
		t.Helper()
		select {
		case p := <-probe.out:
			return p
		case <-ctx.Done():
			require.FailNow(t, "no "+what+" within "+registerWait.String())
			return nil
		}
	}
	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	require.NoError(t, s.handlePacket(context.Background(), control(t, ipscwire.OpRegisterRequest, id), from))
	require.Equal(t, ipscwire.OpRegisterReply, ipscwire.Opcode(next("registration reply")[0]))
	require.NoError(t, <-published)

	for {
		p := next("call that was published at the registration reply")
		if !isUserPacket(ipscwire.Opcode(p[0])) {
			continue
		}
		u, err := ipscwire.DecodeUser(p)
		require.NoError(t, err)
		require.Equal(t, uint32(1000001), u.Source)
		return
	}
}
