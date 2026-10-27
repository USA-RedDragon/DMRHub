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

package dmr_test

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noticeWait = time.Second

func sendPrivateBursts(t *testing.T, c *testutils.MMDVMClient, src, dst, stream uint) {
	t.Helper()
	for i := range 4 {
		pkt := makeGroupVoicePacket(src, dst, stream, false)
		pkt.GroupCall = false
		pkt.Seq = uint(i)
		require.NoError(t, c.SendDMRD(pkt))
		time.Sleep(60 * time.Millisecond)
	}
	term := makeGroupVoiceTermPacket(src, dst, stream, false)
	term.GroupCall = false
	term.Seq = 4
	require.NoError(t, c.SendDMRD(term))
}

func countPrivate(pkts []models.Packet, dst uint) int {
	n := 0
	for _, p := range pkts {
		if !p.GroupCall && p.Dst == dst {
			n++
		}
	}
	return n
}

// noticeMarker is the ID of no talkgroup, user, peer or repeater, that a
// test announces after an edit on replica A: replica B reads the notices
// of a kind in order, so once it took the notice of noticeMarker it took
// the one of the edit.
const noticeMarker = 9999

// noticeTaker waits for replica B to take the notices of the edits on
// replica A, of the kinds that watch reports.
type noticeTaker struct {
	ps    pubsub.PubSub
	taken map[string]chan uint
}

func takeNotices(t *testing.T, ps pubsub.PubSub, watch map[string]func(func(uint)) func()) *noticeTaker {
	t.Helper()
	n := &noticeTaker{ps: ps, taken: map[string]chan uint{}}
	for topic, on := range watch {
		ch := make(chan uint, 16)
		n.taken[topic] = ch
		t.Cleanup(on(func(id uint) {
			if id == noticeMarker {
				ch <- id
			}
		}))
	}
	return n
}

// await announces noticeMarker on topic and waits until replica B took it.
func (n *noticeTaker) await(t *testing.T, topic string) {
	t.Helper()
	require.NoError(t, n.ps.Publish(topic, []byte("notice-test:"+strconv.Itoa(noticeMarker))))
	select {
	case <-n.taken[topic]:
	case <-time.After(handshakeWait):
		require.Failf(t, "replica B did not take the notice", "topic %s", topic)
	}
}

func adminRouter(t *testing.T, stack *testutils.IntegrationStack) (*gin.Engine, testutils.CookieJar) {
	t.Helper()
	testutils.SeedAccount(t, stack.DB, 1000009, "ADM1N", "notice-admin")
	require.NoError(t, stack.DB.Model(&models.User{}).Where("id = ?", 1000009).
		Updates(map[string]any{"admin": true, "super_admin": true}).Error)
	ready := &atomic.Bool{}
	ready.Store(true)
	cfg := *stack.Config
	cfg.DMR.DisableRadioIDValidation = true
	cfg.AutoApprove = true
	router := dmrhttp.CreateRouter(context.Background(), &cfg, stack.Hub, stack.DB, stack.PubSub, ready, nil, "test", "deadbeef")
	return router, testutils.Login(t, router, "notice-admin")
}

// TestEditsOnOneReplicaTakeEffectAtOnceOnAnother makes a talkgroup and a
// user, turns off the egress of a peer and deletes an IPSC repeater
// through the API of replica A, right
// after replica B routed traffic that read each of them. The next call on
// replica B must follow the edit, without waiting for B to read the
// database again on its own.
func TestEditsOnOneReplicaTakeEffectAtOnceOnAnother(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		require.NoError(t, stack.DB.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100002).Error)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)

		replicaB := stack.SpawnSecondReplica(t)
		stack.StartServers(t)
		router, jar := adminRouter(t, stack)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(replicaB.MMDVMAddr))
		require.NoError(t, c2.Connect(replicaB.MMDVMAddr))
		t.Cleanup(c1.Close)
		t.Cleanup(c2.Close)
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))
		to := testutils.NewOpenBridgeClient(500002, obPassword)
		t.Cleanup(to.Close)
		require.NoError(t, to.Connect(replicaB.OpenBridgeAddr))
		addr := to.LocalAddr()
		stack.SeedPeer(t, 500002, 1000001, obPassword, addr.IP.String(), addr.Port, false, true)
		stack.SeedPeerRule(t, 500002, false, 1, 9999999)
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		t.Cleanup(ic.Close)
		require.NoError(t, ic.Connect(replicaB.IPSCAddr))
		require.NoError(t, ic.WaitReady(handshakeWait))
		notices := takeNotices(t, stack.PubSub, map[string]func(func(uint)) func(){
			"hub:talkgroups:changed": replicaB.Hub.OnTalkgroupChange,
			"hub:users:changed":      replicaB.Hub.OnUserChange,
			"hub:peers:changed":      replicaB.Hub.OnPeerChange,
			"hub:repeaters:changed":  replicaB.Hub.OnRepeaterChange,
		})
		time.Sleep(settleDuration)

		func() {
			sendGroupBursts(t, c1, 5, 9101, 2)
			c2.Drain(300 * time.Millisecond)
			resp, w := testutils.CreateTalkgroup(t, router, jar, apimodels.TalkgroupPost{ID: 5, Name: "TG5", Description: "new"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			notices.await(t, "hub:talkgroups:changed")
			sendGroupBursts(t, c2, 5, 9102, 2)
			time.Sleep(300 * time.Millisecond)
			c2.Drain(100 * time.Millisecond)
			sendGroupBursts(t, c1, 5, 9103, 4)
			assert.Positive(t, countDst(c2.Drain(noticeWait), 5), "a talkgroup made on replica A must route on replica B at once")
		}()

		func() {
			sendPrivateBursts(t, c1, 1000001, 1000004, 9201)
			c2.Drain(300 * time.Millisecond)
			resp, w := testutils.RegisterUser(t, router, apimodels.UserRegistration{DMRId: 1000004, Callsign: "N0CAL4", Username: "notice-user", Password: "password1234"})
			require.Equal(t, http.StatusOK, w.Code, resp.Error)
			notices.await(t, "hub:users:changed")
			require.NoError(t, c2.SendDMRD(makeGroupVoicePacket(1000004, 1, 9202, false)))
			time.Sleep(300 * time.Millisecond)
			c2.Drain(100 * time.Millisecond)
			sendPrivateBursts(t, c1, 1000001, 1000004, 9203)
			assert.Positive(t, countPrivate(c2.Drain(noticeWait), 1000004), "a user made on replica A must get private calls on replica B at once")
		}()

		func() {
			to.Drain(100 * time.Millisecond)
			sendGroupBursts(t, c1, 1, 9301, 4)
			require.NotEmpty(t, to.Drain(noticeWait), "the peer must get the call before the edit")
			off := false
			_, w := testutils.PatchPeer(t, router, 500002, jar, apimodels.PeerPatch{Egress: &off})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			notices.await(t, "hub:peers:changed")
			sendGroupBursts(t, c1, 1, 9302, 4)
			assert.Empty(t, to.Drain(noticeWait), "a peer whose egress replica A turned off must get nothing from replica B")
		}()

		func() {
			c2.Drain(100 * time.Millisecond)
			require.NoError(t, ic.SendGroupVoice(1000001, 1, false, 9401))
			require.Positive(t, countDst(c2.Drain(noticeWait), 1), "the IPSC call must route before the delete")
			w := testutils.Do(t, router, jar, http.MethodDelete, "/api/v1/repeaters/200001", nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			notices.await(t, "hub:repeaters:changed")
			require.NoError(t, ic.SendGroupVoice(1000001, 1, false, 9402))
			assert.Zero(t, countDst(c2.Drain(noticeWait), 1), "an IPSC repeater deleted on replica A must route nothing on replica B")
		}()
	})
}
