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
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

// TestHandOverEveryBurstOfASlowPeerMove sends a call of 100 bursts on one
// slot to a peer of a replica that drains, and then hands the peer over
// from before the first burst, as when the move reached the replica late
// on a starved machine. The handover must hold every burst.
func TestHandOverEveryBurstOfASlowPeerMove(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		log := newSendLog()
		p := newPacer(clock.Real(), log.send, nil)
		defer p.stop()
		p.history = func() (time.Duration, int, bool) {
			keep, perSlot := hub.HandoverHistory(25 * time.Second)
			return keep, perSlot, true
		}
		k := slotKey{peer: 311860, slot: false}
		since := time.Now().Add(hub.MoveSlack)
		time.Sleep(hub.MoveSlack + time.Millisecond)
		const bursts = 100
		for i := range bursts {
			p.enqueue(k, 7, []byte{1}, i == bursts-1, models.Packet{StreamID: 7, Seq: uint(i)}, &streamSnapshot{}, time.Time{})
		}
		for range bursts {
			<-log.got
		}
		synctest.Wait()
		_, lost, _ := p.handOver(k.peer, since)
		require.Equal(t, bursts, len(lost), "the handover lost bursts that left after the move")
	})
}

// TestHandOverWhatWaitsForAnEarlierPeerHandover moves a peer on while the
// replica that it moved to still holds bursts for it until the handover
// of the replica before comes. Those bursts never went to the peer, so the
// handover to the next replica must hold them. The in-memory pubsub hands
// the report to the subscription before HandOver returns.
func TestHandOverWhatWaitsForAnEarlierPeerHandover(t *testing.T) {
	t.Parallel()
	const peer = 311861
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	h := hub.NewHub(nil, nil, ps, nil)
	t.Cleanup(func() { h.Stop(context.Background()) })
	sub := ps.Subscribe("hub:handover:" + models.RepeaterTypeIPSC + ":311861")
	t.Cleanup(func() { _ = sub.Close() })
	s := &IPSCServer{clock: clock.Real(), localID: 1, translator: NewIPSCTranslator(1), hub: h, peers: map[uint32]*peerState{peer: {}},
		pacer: newPacer(clock.Real(), func(slotKey, []byte) {}, nil)}
	t.Cleanup(s.pacer.stop)
	m := newMoving()
	m.held = []models.Packet{{StreamID: 7, Seq: 2}, {StreamID: 7, Seq: 3}}
	s.moving = map[uint32]*moving{peer: m}

	s.handOverPeer(peer, time.Now())
	var report handoverReport
	select {
	case b := <-sub.Channel():
		require.NoError(t, json.Unmarshal(b, &report))
	default:
		require.FailNow(t, "no handover report")
	}
	lost := unmarshalPackets(report.Lost)
	seqs := make([]uint, 0, len(lost))
	for _, p := range lost {
		seqs = append(seqs, p.Seq)
	}
	require.Equal(t, []uint{2, 3}, seqs, "the handover lost the bursts that waited for the earlier one")
}
